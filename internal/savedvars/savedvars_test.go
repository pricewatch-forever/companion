package savedvars_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/pricewatch-forever/companion/internal/savedvars"
)

type fixture struct {
	Cases []struct {
		Name  string `json:"name"`
		Input struct {
			Text string `json:"text"`
		} `json:"input"`
		Expected struct {
			Value json.RawMessage `json:"value"`
			Error string          `json:"error"`
		} `json:"expected"`
	} `json:"cases"`
}

func TestFixtureCases(t *testing.T) {
	raw, err := os.ReadFile("../../fixtures/saved-variables.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) == 0 {
		t.Fatal("no cases in the fixture")
	}
	for _, c := range f.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := savedvars.Parse([]byte(c.Input.Text))
			if c.Expected.Error != "" {
				if err == nil {
					t.Fatalf("parsed to %v, want an error about %q", got, c.Expected.Error)
				}
				if !strings.Contains(err.Error(), c.Expected.Error) {
					t.Errorf("error %q does not mention %q", err, c.Expected.Error)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertSameJSON(t, got, c.Expected.Value)
		})
	}
}

// The parsed recording of the PriceWatch.lua case is recording-v1.json's
// first case's expected recording.
func TestPriceWatchCaseMatchesRecordingFixture(t *testing.T) {
	raw, err := os.ReadFile("../../fixtures/recording-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Cases []struct {
			Expected struct {
				Recording json.RawMessage `json:"recording"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../../fixtures/saved-variables.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Cases {
		if !strings.HasPrefix(c.Name, "PriceWatch.lua") {
			continue
		}
		got, err := savedvars.Parse([]byte(c.Input.Text))
		if err != nil {
			t.Fatal(err)
		}
		db := got["PriceWatchDB"].(map[string]any)
		assertSameJSON(t, db["recording"], rec.Cases[0].Expected.Recording)
		return
	}
	t.Fatal("no PriceWatch.lua case in the fixture")
}

func TestALargeNumberKeyIsWrittenInFull(t *testing.T) {
	got, err := savedvars.Parse([]byte("X = {\n\t[1793790000] = true,\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, got, json.RawMessage(`{"X":{"1793790000":true}}`))
}

func TestRefusesWhatTheClientNeverWrites(t *testing.T) {
	for name, text := range map[string]string{
		"a function call":         "X = print(1)\n",
		"an unknown escape":       "X = \"\\q\"\n",
		"a missing value":         "X = \n",
		"a missing name":          "= 1\n",
		"an unclosed table":       "X = {\n\t1,\n",
		"a byte escape over 255":  "X = \"\\256\"\n",
		"text after a value":      "X = 1 2\n",
		"a bare word as a value":  "X = abc\n",
		"a number that isn't one": "X = 1.2.3\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := savedvars.Parse([]byte(text)); err == nil {
				t.Errorf("parsed to %v, want an error", got)
			}
		})
	}
}

func assertSameJSON(t *testing.T, got any, want json.RawMessage) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var g, w any
	if err := json.Unmarshal(gotJSON, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("got  %s\nwant %s", gotJSON, want)
	}
}
