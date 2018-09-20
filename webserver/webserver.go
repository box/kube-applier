package webserver

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/run"
	"github.com/utilitywarehouse/kube-applier/sysutil"

	"github.com/gorilla/mux"
)

const serverTemplatePath = "/templates/status.html"

// WebServer struct
type WebServer struct {
	ListenPort int
	Clock      sysutil.ClockInterface
	RunQueue   chan<- bool
	RunResults <-chan run.Result
	Errors     chan<- error
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
		http.Error(w, "Error: Unable to load HTML template", http.StatusInternalServerError)
		log.Logger.Error("Request failed", "error", http.StatusInternalServerError, "time", s.Clock.Now().String())
		return
	}
	log.Logger.Info("Request completed successfully", "time", s.Clock.Now().String())
}

// ForceRunHandler implements the http.Handle interface and serves an API endpoint for forcing a new run.
type ForceRunHandler struct {
	RunQueue chan<- bool
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
		select {
		case f.RunQueue <- true:
			log.Logger.Info("Run queued")
		default:
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
	lastRun := &run.Result{}

	template, err := sysutil.CreateTemplate(serverTemplatePath)
	if err != nil {
		ws.Errors <- err
		return
	}

	m := mux.NewRouter()
	addStatusEndpoints(m)
	statusPageHandler := &StatusPageHandler{
		template,
		lastRun,
		ws.Clock,
	}
	http.Handle("/", statusPageHandler)
	m.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("/static"))))
	forceRunHandler := &ForceRunHandler{
		ws.RunQueue,
	}
	m.PathPrefix("/api/1.0/forceRun").Handler(forceRunHandler)
	m.PathPrefix("/").Handler(statusPageHandler)

	go func() {
		for result := range ws.RunResults {
			*lastRun = result
		}
	}()

	err = http.ListenAndServe(fmt.Sprintf(":%v", ws.ListenPort), m)
	ws.Errors <- err
}
