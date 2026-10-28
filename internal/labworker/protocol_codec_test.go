package labworker

import (
	"bytes"
	"context"
	"testing"
)

func TestMeasurementCodecIndependentOfExecutionAdapter(t *testing.T) {
	report := realSmallReport(t)
	c := report.Configuration
	request := MeasurementRequest{1, "faultline-worker-measurement-request", c.LaunchID, c.RunID, c.Specification, c.SpecificationSHA256, "off"}
	input, e := request.Encode()
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := DecodeMeasurementRequest(context.Background(), bytes.NewReader(input))
	if e != nil || decoded != request {
		t.Fatal(e)
	}
	again, e := decoded.Encode()
	if e != nil || !bytes.Equal(input, again) {
		t.Fatal("unstable request")
	}
	response := MeasurementResponse{1, "faultline-worker-measurement-response", report}
	output, e := response.Encode()
	if e != nil {
		t.Fatal(e)
	}
	roundtrip, e := DecodeMeasurementResponse(context.Background(), bytes.NewReader(output), request, c.SourceRevision)
	if e != nil {
		t.Fatal(e)
	}
	again, e = roundtrip.Encode()
	if e != nil || !bytes.Equal(output, again) {
		t.Fatal("unstable response")
	}
	for _, bad := range [][]byte{[]byte(`null`), []byte(`{}`), bytes.Replace(input, []byte(`"tracing":"off"`), []byte(`"tracing":null`), 1), bytes.Replace(input, []byte(`"schema_version":1`), []byte(`"Schema_Version":1`), 1), bytes.Replace(input, []byte(`"schema_version":1`), []byte(`"schema_version":1,"\u0073chema_version":1`), 1), bytes.Repeat([]byte(" "), MaxMeasurementInput+1)} {
		if _, e := DecodeMeasurementRequest(context.Background(), bytes.NewReader(bad)); e == nil {
			t.Fatal("malformed request")
		}
	}
	if _, e := DecodeMeasurementResponse(context.Background(), bytes.NewReader(output), request, "bad"); e == nil {
		t.Fatal("bad trusted revision")
	}
}
