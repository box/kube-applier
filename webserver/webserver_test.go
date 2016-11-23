package webserver_test

import (
	"github.com/box/kube-applier/sysutil"
	"github.com/box/kube-applier/webserver"
	"github.com/golang/mock/gomock"
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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

type TestCase struct {
	tmpl         *template.Template
	data         interface{}
	expectedCode int
}

var tests = []TestCase{
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

	for _, test := range tests {
		clock.EXPECT().Now().Times(2).Return(time.Time{})
		handler := webserver.StatusPageHandler{test.tmpl, test.data, clock}
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
