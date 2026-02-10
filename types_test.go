package thingscloud

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBoolean_UnmarshalJSON(t *testing.T) {
	testCases := []struct {
		JSON     string
		Expected bool
	}{
		{"1", true},
		{"0", false},
	}
	for _, testCase := range testCases {
		bs := []byte(testCase.JSON)
		var b Boolean
		if err := json.Unmarshal(bs, &b); err != nil {
			t.Fatal(err.Error())
		}
		if bool(b) != testCase.Expected {
			t.Fatalf("Expected %t but got %t", testCase.Expected, b)
		}
	}
}

func TestBoolean_MarshalJSON(t *testing.T) {
	testCases := []struct {
		Value    bool
		Expected string
	}{
		{true, "1"},
		{false, "0"},
	}
	for _, testCase := range testCases {
		b := Boolean(testCase.Value)
		bb := &b
		bs, err := bb.MarshalJSON()
		if err != nil {
			t.Fatal(err.Error())
		}
		if string(bs) != testCase.Expected {
			t.Fatalf("Expected %q but got %q", testCase.Expected, string(bs))
		}
	}
}

func TestTaskType_Values(t *testing.T) {
	if TaskTypeTask != 0 {
		t.Errorf("expected TaskTypeTask=0, got %d", TaskTypeTask)
	}
	if TaskTypeProject != 1 {
		t.Errorf("expected TaskTypeProject=1, got %d", TaskTypeProject)
	}
	if TaskTypeHeading != 2 {
		t.Errorf("expected TaskTypeHeading=2, got %d", TaskTypeHeading)
	}
}

func TestTaskType_JSONRoundTrip(t *testing.T) {
	type wrapper struct {
		TP *TaskType `json:"tp"`
	}
	tp := TaskTypeHeading
	w := wrapper{TP: &tp}
	bs, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if string(bs) != `{"tp":2}` {
		t.Errorf("expected {\"tp\":2}, got %s", string(bs))
	}
	var w2 wrapper
	json.Unmarshal(bs, &w2)
	if *w2.TP != TaskTypeHeading {
		t.Errorf("expected TaskTypeHeading, got %d", *w2.TP)
	}
}

func TestTimestamp_UnmarshalJSON(t *testing.T) {
	testCases := []struct {
		JSON     string
		Expected string
	}{
		{"1496001956.2693141", "2017-05-28T20:05:17"},
		{"1496001956", "2017-05-28T20:05:17"},
	}
	for _, testCase := range testCases {
		bs := []byte(testCase.JSON)
		var tt Timestamp
		if err := json.Unmarshal(bs, &tt); err != nil {
			t.Fatal(err.Error())
		}
		if tt.Format("2006-01-02T15:04:06") != testCase.Expected {
			t.Fatalf("Expected %q but got %q", testCase.Expected, tt.Format("2006-01-02T15:04:06"))
		}
	}
}

func TestTimestamp_MarshalJSON(t *testing.T) {
	testCases := []struct {
		Time     time.Time
		Expected string
	}{
		{time.Date(2017, time.May, 28, 22, 05, 17, 0, time.UTC), "1496009117"},
		{time.Time{}, "-62135596800"},
	}
	for _, testCase := range testCases {

		tt := Timestamp(testCase.Time)
		ttt := &tt
		bs, err := ttt.MarshalJSON()
		if err != nil {
			t.Fatal(err.Error())
		}
		if string(bs) != testCase.Expected {
			t.Fatalf("Expected %q but got %q", testCase.Expected, string(bs))
		}
	}
}
