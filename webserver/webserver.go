package webserver

import (
	"encoding/json"
	"fmt"
	"github.com/box/kube-applier/run"
	"github.com/box/kube-applier/sysutil"
	"html/template"
	"log"
	"net/http"
)

const serverTemplatePath = "/templates/status.html"

type WebServer struct {
	ListenPort     int
	Clock          sysutil.ClockInterface
	MetricsHandler http.Handler
	FullRunQueue   chan<- bool
	RunResults     <-chan run.Result
	Errors         chan<- error
}

// StatusPageHandler implements the http.Handler interface and serves a status page with info about the most recent applier run.
type StatusPageHandler struct {
	Template *template.Template
	Data     interface{}
	Clock    sysutil.ClockInterface
}

// ServeHTTP populates the status page template with data and serves it when there is a request.
func (s *StatusPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("Applier status request at %s", s.Clock.Now().String())
	if s.Template == nil {
		handleTemplateError(w, fmt.Errorf("No template found"), s.Clock)
		return
	}
	if err := s.Template.Execute(w, s.Data); err != nil {
		handleTemplateError(w, err, s.Clock)
		return
	}
	log.Printf("Request completed successfully at %s", s.Clock.Now().String())
}

func handleTemplateError(w http.ResponseWriter, err error, clock sysutil.ClockInterface) {
	log.Printf("Error applying template: %v", err)
	http.Error(w, "Error: Unable to load HTML template", http.StatusInternalServerError)
	log.Printf("Request failed with error code %v at %s", http.StatusInternalServerError, clock.Now().String())
}

// ForceRunHandler implements the http.Handle interface and serves an API endpoint for forcing a new run.
type ForceRunHandler struct {
	FullRunQueue chan<- bool
}

// ServeHTTP handles requests for forcing a run by attempting to add to the runQueue, and writes a response including the result and a relevant message.
func (f *ForceRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Print("Full run requested by webserver.")
	var data struct {
		Result  string `json:"result"`
		Message string `json:"message"`
	}

	switch r.Method {
	case "POST":
		select {
		case f.FullRunQueue <- true:
			log.Print("Full run queued.")
		default:
			log.Print("Full run queue is already full.")
		}
		data.Result = "success"
		data.Message = "Run queued, will begin upon completion of current run."
		w.WriteHeader(http.StatusOK)
	default:
		data.Result = "error"
		data.Message = "Error: force rejected, must be a POST request."
		w.WriteHeader(http.StatusBadRequest)
		log.Print(data.Message)
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	json.NewEncoder(w).Encode(data)
}

// Init starts the webserver using the given port, and sets up handlers for:
// 1. Status page
// 2. Metrics
// 3. Static content
// 4. Endpoint for forcing a run
func (ws *WebServer) Start() {
	log.Println("Launching webserver")
	lastRun := &run.Result{RunID: -1}

	template, err := sysutil.CreateTemplate(serverTemplatePath)
	if err != nil {
		ws.Errors <- err
		return
	}

	statusPageHandler := &StatusPageHandler{template, lastRun, ws.Clock}
	http.Handle("/", statusPageHandler)
	http.Handle("/metrics", ws.MetricsHandler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	forceRunHandler := &ForceRunHandler{ws.FullRunQueue}
	http.Handle("/api/v1/forceRun", forceRunHandler)

	go func() {
		for result := range ws.RunResults {
			// If the new result is from a run that started later than the currently displayed run, update the page.
			// Otherwise, a run with info from an older commit might replace a newer commit.
			if result.RunID > lastRun.RunID {
				log.Printf("Updating status page with info from Run %v.", result.RunID)
				*lastRun = result
			}
		}
	}()

	err = http.ListenAndServe(fmt.Sprintf(":%v", ws.ListenPort), nil)
	ws.Errors <- err
}
