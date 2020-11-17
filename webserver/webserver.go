package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/client"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/run"
	"github.com/utilitywarehouse/kube-applier/sysutil"
)

const serverTemplatePath = "templates/status.html"

// WebServer struct
type WebServer struct {
	ApplicationPollInterval time.Duration
	ListenPort              int
	Clock                   sysutil.ClockInterface
	DiffURLFormat           string
	KubeClient              *client.Client
	RunQueue                chan<- run.Request
	result                  *Result
	server                  *http.Server
	stop, stopped           chan bool
}

// StatusPageHandler implements the http.Handler interface and serves a status page with info about the most recent applier run.
type StatusPageHandler struct {
	Clock    sysutil.ClockInterface
	Result   *Result
	Template *template.Template
}

// ServeHTTP populates the status page template with data and serves it when there is a request.
func (s *StatusPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Logger.Info("Applier status request", "time", s.Clock.Now().String())
	if s.Template == nil {
		http.Error(w, "Error: Unable to load HTML template", http.StatusInternalServerError)
		log.Logger.Error("Request failed", "error", "No template found", "time", s.Clock.Now().String())
		return
	}
	if err := s.Template.Execute(w, s.Result); err != nil {
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
			data.Message = fmt.Sprintf("Cannot find Applications in namespace '%s'", ns)
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
		data.Message = "Run queued"
		w.WriteHeader(http.StatusOK)
	default:
		data.Result = "error"
		data.Message = "Must be a POST request"
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
func (ws *WebServer) Start() error {
	if ws.server != nil {
		return fmt.Errorf("WebServer already running")
	}

	ws.stop = make(chan bool)
	ws.stopped = make(chan bool)

	log.Logger.Info("Launching webserver")

	template, err := sysutil.CreateTemplate(serverTemplatePath)
	if err != nil {
		return err
	}

	ws.result = &Result{
		Mutex:         &sync.Mutex{},
		DiffURLFormat: ws.DiffURLFormat,
	}

	m := mux.NewRouter()
	addStatusEndpoints(m)
	statusPageHandler := &StatusPageHandler{
		ws.Clock,
		ws.result,
		template,
	}
	forceRunHandler := &ForceRunHandler{
		ws.KubeClient,
		ws.RunQueue,
	}
	m.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	m.PathPrefix("/api/v1/forceRun").Handler(forceRunHandler)
	m.PathPrefix("/").Handler(statusPageHandler)

	go func() {
		ticker := time.NewTicker(ws.ApplicationPollInterval)
		defer ticker.Stop()
		defer close(ws.stopped)
		for {
			select {
			case <-ticker.C:
				ws.updateResult()
			case <-ws.stop:
				return
			}
		}
	}()

	ws.server = &http.Server{
		Addr:     fmt.Sprintf(":%v", ws.ListenPort),
		Handler:  m,
		ErrorLog: log.Logger.StandardLogger(nil),
	}

	go func() {
		if err = ws.server.ListenAndServe(); err != nil {
			log.Logger.Error(fmt.Sprintf("webserver error: %v", err))
		}
	}()

	return nil
}

func (ws *WebServer) Shutdown() error {
	close(ws.stop)
	<-ws.stopped
	err := ws.server.Shutdown(context.TODO())
	ws.server = nil
	return err
}

func (ws *WebServer) updateResult() error {
	apps, err := ws.KubeClient.ListApplications(context.TODO())
	if err != nil {
		return fmt.Errorf("Could not list Application resources: %v", err)
	}
	ws.result.Lock()
	ws.result.Applications = apps
	ws.result.Unlock()
	return nil
}
