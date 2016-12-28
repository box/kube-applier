package webserver

import (
	"fmt"
	"github.com/box/kube-applier/sysutil"
	"html/template"
	"log"
	"net/http"
)

const serverTemplatePath = "/templates/status.html"

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

// StartWebServer initializes the webserver using the given port, and sets up handlers for:
// 1. Status page
// 2. Metrics
// 3. Static content
// 4. Endpoint for forcing a run
func StartWebServer(listenPort int, data interface{}, clock sysutil.ClockInterface, metricsHandler http.Handler, forceSwitch *bool) error {
	log.Println("Launching webserver")

	template, err := sysutil.CreateTemplate(serverTemplatePath)
	if err != nil {
		return err
	}

	handler := &StatusPageHandler{template, data, clock}
	http.Handle("/", handler)
	http.Handle("/metrics", metricsHandler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/force", func(w http.ResponseWriter, r *http.Request) {
		*forceSwitch = true
	})

	err = http.ListenAndServe(fmt.Sprintf(":%v", listenPort), nil)
	return err
}
