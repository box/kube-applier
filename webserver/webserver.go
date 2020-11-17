package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/client"
	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/run"
	"github.com/utilitywarehouse/kube-applier/sysutil"
)

const serverTemplatePath = "templates/status.html"

// WebServer struct
type WebServer struct {
	ListenPort    int
	Clock         sysutil.ClockInterface
	DiffURLFormat string
	KubeClient    *client.Client
	RepoPath      string
	RunQueue      chan<- run.Request
	Errors        chan<- error
	// TODO: how do we prevent races here? mutex?
	result Result
}

// StatusPageHandler implements the http.Handler interface and serves a status page with info about the most recent applier run.
type StatusPageHandler struct {
	Template *template.Template
	Data     interface{}
	Clock    sysutil.ClockInterface
}

// ServeHTTP populates the status page template with data and serves it when there is a request.
func (s *StatusPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Logger.Info("Applier status request", "time", s.Clock.Now().String())
	if s.Template == nil {
		http.Error(w, "Error: Unable to load HTML template", http.StatusInternalServerError)
		log.Logger.Error("Request failed", "error", "No template found", "time", s.Clock.Now().String())
		return
	}
	if err := s.Template.Execute(w, s.Data); err != nil {
		http.Error(w, "Error: Unable to render HTML template", http.StatusInternalServerError)
		log.Logger.Error("Request failed", "error", http.StatusInternalServerError, "time", s.Clock.Now().String(), "err", err)
		return
	}
	log.Logger.Info("Request completed successfully", "time", s.Clock.Now().String())
}

// ForceRunHandler implements the http.Handle interface and serves an API endpoint for forcing a new run.
type ForceRunHandler struct {
	KubeClient *client.Client
	RunQueue   chan<- run.Request
}

// ServeHTTP handles requests for forcing a run by attempting to add to the runQueue, and writes a response including the result and a relevant message.
func (f *ForceRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Logger.Info("Force run requested")
	var data struct {
		Result  string `json:"result"`
		Message string `json:"message"`
	}

	switch r.Method {
	case "POST":
		if err := r.ParseForm(); err != nil {
			data.Result = "error"
			data.Message = "Could not parse form data"
			log.Logger.Error(data.Message)
			w.WriteHeader(http.StatusBadRequest)
			break
		}

		ns := r.FormValue("namespace")
		if ns == "" {
			data.Result = "error"
			data.Message = "Empty namespace value"
			log.Logger.Error(data.Message)
			w.WriteHeader(http.StatusBadRequest)
			break
		}

		apps, err := f.KubeClient.ListApplications(context.TODO())
		if err != nil {
			data.Result = "error"
			data.Message = "Cannot list Applications"
			w.WriteHeader(http.StatusInternalServerError)
			break
		}

		var app *kubeapplierv1alpha1.Application
		for i := range apps {
			if apps[i].Namespace == ns {
				app = &apps[i]
				// TODO: handle multiple applications in one namespace. The
				// behaviour should match that of run.Scheduler, which can also
				// queue requests.
			}
		}
		if app == nil {
			data.Result = "error"
			data.Message = fmt.Sprintf("Cannot find Applications in namespace %s", ns)
			w.WriteHeader(http.StatusBadRequest)
			break
		}

		runRequest := run.Request{
			Type:        run.ForcedRun,
			Application: app,
		}

		select {
		case f.RunQueue <- runRequest:
			log.Logger.Info("Run queued")
			// TODO: remove timeout, we should not lose any requests
		case <-time.After(5 * time.Second):
			log.Logger.Info("Run queue is already full")
		}
		data.Result = "success"
		data.Message = "Run queued, will begin upon completion of current run."
		w.WriteHeader(http.StatusOK)
	default:
		data.Result = "error"
		data.Message = "Error: force rejected, must be a POST request."
		w.WriteHeader(http.StatusBadRequest)
		log.Logger.Info(data.Message)
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	json.NewEncoder(w).Encode(data)
}

// Start starts the webserver using the given port, and sets up handlers for:
// 1. Status page
// 2. Metrics
// 3. Static content
// 4. Endpoint for forcing a run
func (ws *WebServer) Start() {
	log.Logger.Info("Launching webserver")

	template, err := sysutil.CreateTemplate(serverTemplatePath)
	if err != nil {
		ws.Errors <- err
		return
	}

	m := mux.NewRouter()
	addStatusEndpoints(m)
	statusPageHandler := &StatusPageHandler{
		template,
		ws.result,
		ws.Clock,
	}
	// TODO: why do we register this with http?
	http.Handle("/", statusPageHandler)
	m.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	forceRunHandler := &ForceRunHandler{
		ws.KubeClient,
		ws.RunQueue,
	}
	m.PathPrefix("/api/v1/forceRun").Handler(forceRunHandler)
	m.PathPrefix("/").Handler(statusPageHandler)

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		select {
		case <-ticker.C:
			ws.initialiseResultFromKubernetes()
		}
	}()

	err = http.ListenAndServe(fmt.Sprintf(":%v", ws.ListenPort), m)
	ws.Errors <- err
}

func (ws *WebServer) initialiseResultFromKubernetes() error {
	gitUtil := &git.Util{RepoPath: ws.RepoPath}
	res := Result{
		DiffURLFormat: ws.DiffURLFormat,
		RootPath:      ws.RepoPath,
	}
	apps, err := ws.KubeClient.ListApplications(context.TODO())
	if err != nil {
		return fmt.Errorf("Could not list Application resources: %v", err)
	}
	for _, app := range apps {
		// TODO: what do we do with these, they should probably be added to the list?
		if app.Status.LastRun != nil {
			res.Applications = append(res.Applications, app)
			if app.Status.LastRun.Info.Started.After(res.LastRun.Started.Time) {
				res.LastRun = app.Status.LastRun.Info
			}
		}
	}
	if res.LastRun.Commit != "" {
		commitLog, err := gitUtil.CommitLog(res.LastRun.Commit)
		if err != nil {
			log.Logger.Warn(fmt.Sprintf("Could not get commit message for commit %s: %v", res.LastRun.Commit, err))
		}
		res.FullCommit = commitLog
	}
	ws.result = res
	return nil
}
