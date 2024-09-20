package webserver

import (
	"github.com/box/kube-applier/sysutil"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	successBody = "{\"result\":\"success\",\"message\":\"Run queued, will begin upon completion of current run.\"}\n"
	errorBody   = "{\"result\":\"error\",\"message\":\"Error: force rejected, must be a POST request.\"}\n"
)

// **** Tests for Status Page Handler ****
type mockData struct {
	IntField    int
	StringField string
	TimeField   time.Time
}

func mockTemplate(rawTemplate string) *template.Template {
	tmpl, err := template.New("").Parse(rawTemplate)
	if err != nil {
		return nil
	}
	return tmpl
}

type StatusPageTestCase struct {
	tmpl         *template.Template
	data         interface{}
	expectedCode int
}

var statusPageTests = []StatusPageTestCase{
	{
		// Empty template, empty data
		mockTemplate(""),
		mockData{},
		http.StatusOK,
	},
	{
		// Nil template, empty data
		// For malformed template formatting, mockTemplate will return nil.
		// It would be redundant to have a case like tmpl = mockTemplate("{{"), as that would reduce to tmpl = nil.
		nil,
		mockData{},
		http.StatusInternalServerError,
	},
	{
		// Empty template, nil data
		mockTemplate(""),
		nil,
		http.StatusOK,
	},
	{
		// Valid mock template
		mockTemplate("{{.IntField}}"),
		mockData{IntField: 1},
		http.StatusOK,
	},
	{
		// Valid mock template with multiple fields
		mockTemplate("{{.IntField}} {{.StringField}}"),
		mockData{IntField: 3, StringField: "test"},
		http.StatusOK,
	},
	{
		// Valid mock template with unused data
		mockTemplate("{{.IntField}}"),
		mockData{IntField: 4, StringField: "test", TimeField: time.Now()},
		http.StatusOK,
	},
	{
		// Missing data field -> template is valid, but template.Execute will fail
		mockTemplate("{{.MissingField}}"),
		mockData{IntField: 4},
		http.StatusInternalServerError,
	},
}

func TestStatusPageHandlerServeHTTP(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	clock := sysutil.NewMockClockInterface(mockCtrl)

	for _, test := range statusPageTests {
		clock.EXPECT().Now().Times(2).Return(time.Time{})
		handler := StatusPageHandler{test.tmpl, test.data, clock}
		req, _ := http.NewRequest("GET", "", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != test.expectedCode {
			t.Errorf(
				"Template: %v\n"+
					"Data: %v\n"+
					"Expected code: %v\n"+
					"Actual code: %v\n",
				test.tmpl,
				test.data,
				test.expectedCode,
				w.Code,
			)
		}
	}
}

// **** Tests for Force Run Handler ****
func TestForceRunHandlerServeHTTP(t *testing.T) {
	runQueue := make(chan bool, 1)
	handler := ForceRunHandler{runQueue}

	// GET request gives an error.
	RequestAndExpect(t, handler, errorBody, "GET")

	// Force run request succeeds (empty queue).
	RequestAndExpect(t, handler, successBody, "POST")

	// Force run request succeeds (queue full).
	RequestAndExpect(t, handler, successBody, "POST")

	// Empty the queue channel.
	<-runQueue

	// Force run request succeeds (empty queue).
	RequestAndExpect(t, handler, successBody, "POST")
}

func RequestAndExpect(t *testing.T, handler ForceRunHandler, expectedBody, requestType string) {
	assert := assert.New(t)
	req, _ := http.NewRequest(requestType, "", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(expectedBody, w.Body.String())
}
