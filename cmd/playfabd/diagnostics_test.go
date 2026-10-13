package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestTelemetryObserverPrintsOnlyNumericErrors(t *testing.T) {
	s, _, _ := testService(t)
	login := loggedIn(t, s)
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	body := `{"Events":[{"Name":"private-event-name","Payload":{"ErrorCode":503,"ErrorDetail":42,"Message":"private-body-message","IdentityToken":"private-identity-token","nested":{"result":5,"StatusCode":401,"errorCode":"private-error-string"}}},{"Name":"private-name-json","PayloadJSON":"{\"ErrorCode\":10,\"SessionTicket\":\"private-ticket\"}"}]}`
	w := call(s, "/Event/WriteTelemetryEvents", body, map[string]string{"X-EntityToken": login.EntityToken.EntityToken})
	if w.Code != 503 {
		t.Fatal("observer must not advertise a successful write")
	}
	for _, expected := range []string{"errorcode=503", "errordetail=42", "result=5", "statuscode=401", "errorcode=10", "/Event/WriteTelemetryEvents"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("missing %s", expected)
		}
	}
	for _, private := range []string{"private-event-name", "private-name-json", "private-body-message", "private-identity-token", "private-error-string", "private-ticket", login.EntityToken.EntityToken} {
		if strings.Contains(logs.String(), private) {
			t.Fatal("private value was logged")
		}
	}
	logs.Reset()
	if w := call(s, "/Event/WriteTelemetryEvents", body, map[string]string{"X-EntityToken": login.SessionTicket}); w.Code != 401 {
		t.Fatal("session ticket accepted as entity credential")
	}
	if strings.Contains(logs.String(), "Party telemetry event=") {
		t.Fatal("unproven diagnostics were logged")
	}
}
