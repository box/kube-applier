// Package webserver implements the Webserver struct which can serve the
// kube-applier status page and prometheus metrics, as well as receive run
// requests from users.
package webserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

const (
	defaultServerTemplatePath = "templates/status.html"
)

// WebServer struct
type WebServer struct {
	Clock                sysutil.ClockInterface
	DiffURLFormat        string
	KubeClient           *client.Client
	ListenPort           int
	RunQueue             chan<- run.Request
	StatusUpdateInterval time.Duration
	TemplatePath         string
	result               *Result
	server               *http.Server
	stop, stopped        chan bool
}

// StatusPageHandler implements the http.Handler interface and serves a status page with info about the most recent applier run.
type StatusPageHandler struct {
	Clock    sysutil.ClockInterface
	Result   *Result
	Template *template.Template
}

// ServeHTTP populates the status page template with data and serves it when there is a request.
func (s *StatusPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Logger("webserver").Info("Applier status request", "time", s.Clock.Now().String())
	if s.Template == nil {
		http.Error(w, "Error: Unable to load HTML template", http.StatusInternalServerError)
		log.Logger("webserver").Error("Request failed", "error", "No template found", "time", s.Clock.Now().String())
		return
	}
	rendered := &bytes.Buffer{}
	if err := s.Template.Execute(rendered, s.Result); err != nil {
		http.Error(w, "Error: Unable to render HTML template", http.StatusInternalServerError)
		log.Logger("webserver").Error("Request failed", "error", http.StatusInternalServerError, "time", s.Clock.Now().String(), "err", err)
		return
	}
	w.WriteHeader(http.StatusOK)
	if _, err := rendered.WriteTo(w); err != nil {
		log.Logger("webserver").Error("Request failed", "error", http.StatusInternalServerError, "time", s.Clock.Now().String(), "err", err)
	}
	log.Logger("webserver").Info("Request completed successfully", "time", s.Clock.Now().String())
}

// ForceRunHandler implements the http.Handle interface and serves an API endpoint for forcing a new run.
type ForceRunHandler struct {
	KubeClient *client.Client
	RunQueue   chan<- run.Request
}

// ServeHTTP handles requests for forcing a run by attempting to add to the runQueue, and writes a response including the result and a relevant message.
func (f *ForceRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Logger("webserver").Info("Force run requested")
	var data struct {
		Result  string `json:"result"`
		Message string `json:"message"`
	}

	switch r.Method {
	case "POST":
		if err := r.ParseForm(); err != nil {
			data.Result = "error"
			data.Message = "Could not parse form data"
			log.Logger("webserver").Error("Could not process force run request", "error", data.Message)
			w.WriteHeader(http.StatusBadRequest)
			break
		}

		ns := r.FormValue("namespace")
		if ns == "" {
			data.Result = "error"
			data.Message = "Empty namespace value"
			log.Logger("webserver").Error("Could not process force run request", "error", data.Message)
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
				break
			}
		}
		if app == nil {
			data.Result = "error"
			data.Message = fmt.Sprintf("Cannot find Applications in namespace '%s'", ns)
			w.WriteHeader(http.StatusBadRequest)
			break
		}

		run.Enqueue(f.RunQueue, run.ForcedRun, app)

		data.Result = "success"
		data.Message = "Run queued"
		w.WriteHeader(http.StatusOK)
	default:
		data.Result = "error"
		data.Message = "Must be a POST request"
		w.WriteHeader(http.StatusBadRequest)
		log.Logger("webserver").Error("Could not process force run request", "error", data.Message)
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

	log.Logger("webserver").Info("Launching")

	templatePath := ws.TemplatePath
	if templatePath == "" {
		templatePath = defaultServerTemplatePath
	}
	template, err := sysutil.CreateTemplate(templatePath)
	if err != nil {
		return err
	}

	ws.result = &Result{
		Mutex:         &sync.Mutex{},
		DiffURLFormat: ws.DiffURLFormat,
	}

	go func() {
		ticker := time.NewTicker(ws.StatusUpdateInterval)
		defer ticker.Stop()
		defer close(ws.stopped)
		ws.updateResult()
		for {
			select {
			case <-ticker.C:
				ws.updateResult()
			case <-ws.stop:
				return
			}
		}
	}()

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

	ws.server = &http.Server{
		Addr:     fmt.Sprintf(":%v", ws.ListenPort),
		Handler:  m,
		ErrorLog: log.Logger("http.Server").StandardLogger(nil),
	}

	go func() {
		if err = ws.server.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				log.Logger("webserver").Error("Shutdown", "error", err)
			}
			log.Logger("webserver").Info("Shutdown")
		}
	}()

	return nil
}

// Shutdown gracefully shuts the webserver down.
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
