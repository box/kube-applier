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
	"github.com/utilitywarehouse/kube-applier/webserver/oidc"
)

const (
	defaultServerTemplatePath = "templates/status.html"
)

// WebServer struct
type WebServer struct {
	Authenticator        *oidc.Authenticator
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

// StatusPageHandler implements the http.Handler interface and serves a status
// page with info about the most recent applier run.
type StatusPageHandler struct {
	Authenticator *oidc.Authenticator
	Clock         sysutil.ClockInterface
	Result        *Result
	Template      *template.Template
}

// ServeHTTP populates the status page template with data and serves it when
// there is a request.
func (s *StatusPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.Authenticator != nil {
		_, err := s.Authenticator.Authenticate(r.Context(), w, r)
		if errors.Is(err, oidc.ErrRedirectRequired) {
			return
		}
		if err != nil {
			http.Error(w, "Error: Authentication failed", http.StatusInternalServerError)
			log.Logger("webserver").Error("Authentication failed", "error", err, "time", s.Clock.Now().String())
			return
		}
	}

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

// ForceRunHandler implements the http.Handle interface and serves an API
// endpoint for forcing a new run.
type ForceRunHandler struct {
	Authenticator *oidc.Authenticator
	KubeClient    *client.Client
	RunQueue      chan<- run.Request
}

// ServeHTTP handles requests for forcing a run by attempting to add to the
// runQueue, and writes a response including the result and a relevant message.
func (f *ForceRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Logger("webserver").Info("Force run requested")
	var data struct {
		Result  string `json:"result"`
		Message string `json:"message"`
	}

	switch r.Method {
	case "POST":
		var (
			userEmail string
			err       error
		)
		if f.Authenticator != nil {
			userEmail, err = f.Authenticator.UserEmail(r.Context(), r)
			if err != nil {
				data.Result = "error"
				data.Message = "not authenticated"
				log.Logger("webserver").Error(data.Message, "error", err)
				w.WriteHeader(http.StatusForbidden)
				break
			}
		}

		if err := r.ParseForm(); err != nil {
			data.Result = "error"
			data.Message = "could not parse form data"
			log.Logger("webserver").Error(data.Message, "error", err)
			w.WriteHeader(http.StatusBadRequest)
			break
		}

		ns := r.FormValue("namespace")
		if ns == "" {
			data.Result = "error"
			data.Message = "empty namespace value"
			log.Logger("webserver").Error(data.Message)
			w.WriteHeader(http.StatusBadRequest)
			break
		}

		waybills, err := f.KubeClient.ListWaybills(r.Context())
		if err != nil {
			data.Result = "error"
			data.Message = "cannot list Waybills"
			log.Logger("webserver").Error(data.Message, "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			break
		}

		var waybill *kubeapplierv1alpha1.Waybill
		for i := range waybills {
			if waybills[i].Namespace == ns {
				waybill = &waybills[i]
				break
			}
		}
		if waybill == nil {
			data.Result = "error"
			data.Message = fmt.Sprintf("cannot find Waybills in namespace '%s'", ns)
			w.WriteHeader(http.StatusBadRequest)
			break
		}

		if f.Authenticator != nil {
			// if the user can patch the Waybill, they are allowed to force a run
			hasAccess, err := f.KubeClient.HasAccess(r.Context(), waybill, userEmail, "patch")
			if !hasAccess {
				data.Result = "error"
				data.Message = fmt.Sprintf("user %s is not allowed to force a run on waybill %s/%s", userEmail, waybill.Namespace, waybill.Name)
				if err != nil {
					log.Logger("webserver").Error(data.Message, "error", err)
				}
				w.WriteHeader(http.StatusForbidden)
				break
			}
		}

		run.Enqueue(f.RunQueue, run.ForcedRun, waybill)
		data.Result = "success"
		data.Message = "Run queued"
		w.WriteHeader(http.StatusOK)
	default:
		data.Result = "error"
		data.Message = "must be a POST request"
		w.WriteHeader(http.StatusBadRequest)
	}

	w.Header().Set("Content-Type", "waybill/json; charset=UTF-8")
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
	template, err := createTemplate(templatePath)
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
		ws.Authenticator,
		ws.Clock,
		ws.result,
		template,
	}
	forceRunHandler := &ForceRunHandler{
		ws.Authenticator,
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
	err := ws.server.Shutdown(context.Background())
	ws.server = nil
	return err
}

func (ws *WebServer) updateResult() error {
	ctx, cancel := context.WithTimeout(context.Background(), ws.StatusUpdateInterval-time.Second)
	defer cancel()
	waybills, err := ws.KubeClient.ListWaybills(ctx)
	if err != nil {
		return fmt.Errorf("Could not list Waybill resources: %v", err)
	}
	ws.result.Lock()
	ws.result.Waybills = waybills
	ws.result.Unlock()
	return nil
}
