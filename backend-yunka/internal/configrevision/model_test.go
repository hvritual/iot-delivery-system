package configrevision

import (
	"strings"
	"testing"
)

func TestNormalizePayloadCanonicalizesNumbersWithoutPrecisionLoss(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
		want    string
	}{
		{name: "integer", payload: `{"number":100}`, want: `{"number":100}`},
		{name: "decimal equivalent", payload: `{"number":100.0}`, want: `{"number":100}`},
		{name: "positive exponent", payload: `{"number":1e2}`, want: `{"number":100}`},
		{name: "positive exponent alternative", payload: `{"number":10e1}`, want: `{"number":100}`},
		{name: "trim fractional zeroes", payload: `{"number":1.2300}`, want: `{"number":1.23}`},
		{name: "negative exponent fractional", payload: `{"number":123e-2}`, want: `{"number":1.23}`},
		{name: "small decimal", payload: `{"number":0.00100}`, want: `{"number":0.001}`},
		{name: "small exponent", payload: `{"number":1e-3}`, want: `{"number":0.001}`},
		{name: "negative zero integer", payload: `{"number":-0}`, want: `{"number":0}`},
		{name: "negative zero decimal", payload: `{"number":-0.0}`, want: `{"number":0}`},
		{name: "large integer trailing zeroes", payload: `{"number":900719925474099312340}`, want: `{"number":900719925474099312340}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, _, err := normalizePayload(test.payload)
			if err != nil || got != test.want {
				t.Fatalf("canonical payload = %q error=%v, want %q", got, err, test.want)
			}
		})
	}
	first, firstHash, err := normalizePayload(`{"number":100}`)
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := normalizePayload(`{"number":101}`)
	if err != nil || first == second || firstHash == secondHash {
		t.Fatalf("different numbers must have distinct canonical payload/hash: %q %q %x %x error=%v", first, second, firstHash, secondHash, err)
	}
}

func TestNormalizePayloadRejectsOversizedCanonicalOutputAndSensitiveIdentifiers(t *testing.T) {
	oversized := `{"text":"` + strings.Repeat("<", maxPayloadBytes/4) + `"}`
	if _, _, err := normalizePayload(oversized); err == nil {
		t.Fatal("oversized canonical payload unexpectedly passed")
	}
	for _, key := range []string{"db_password", "auth.token", "client secret", "metadata.credentials"} {
		if _, _, err := normalizePayload(`{"` + key + `":"not-a-secret"}`); err == nil {
			t.Fatalf("sensitive field name %q unexpectedly passed", key)
		}
	}
	for _, mutate := range []func(*AppendInput){
		func(input *AppendInput) { input.ID = "id with space" },
		func(input *AppendInput) { input.OrganizationID = "Bearer credential" },
		func(input *AppendInput) { input.ConfigKey = "svc.token" },
		func(input *AppendInput) { input.CreatedByID = "user\nname" },
	} {
		input := appendInput("valid-id", `{}`, 0)
		mutate(&input)
		if err := validateAppendInput(input); err == nil {
			t.Fatalf("invalid identifier %#v unexpectedly passed", input)
		}
	}
}
