# Things Cloud SDK Update — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Update the Things Cloud SDK to match the current Things 3 API (schema 301, build 32209501), fixing outdated types, adding missing fields, and introducing new capabilities discovered via HAR capture.

**Architecture:** The SDK is event-sourced — all mutations produce `Item` objects committed to a `History`. We update the payload structs and domain types to match the real API schema, add a new `Tombstone2` entity kind, a `TaskType` enum replacing the boolean `IsProject`, structured notes, and a device registration endpoint. Changes flow through three layers: types (payload structs + domain structs) → state aggregation (`state/memory`) → client (HTTP methods + headers).

**Tech Stack:** Go 1.18, standard library `net/http`, `encoding/json`, `github.com/google/uuid`

---

## Findings from HAR Capture Analysis

The Proxyman capture (`assets/Things_02-10-2026-10-01-09.har`) contains 91 requests against `cloud.culturedcode.com`. Three unique endpoint patterns were observed:

| Method | Endpoint | Count | SDK Status |
|--------|----------|-------|------------|
| `GET` | `/version/1/history/{key}/items?start-index=N` | 56 | Exists |
| `POST` | `/version/1/history/{key}/commit?ancestor-index=N&_cnt=1` | 34 | Exists |
| `PUT` | `/version/1/app-instance/{app-instance-id}` | 1 | **Missing** |

### Key Differences Between SDK and Current API

1. **User-Agent outdated**: SDK sends `ThingsMac/31516502`, API now uses `ThingsMac/32209501`
2. **`tp` field is an int enum, not a boolean**: `0`=task, `1`=project, `2`=heading. SDK uses `Boolean` type
3. **Notes are structured JSON, not XML strings**: Format is `{"_t":"tx","t":1,"ch":<checksum>,"v":"text"}` with delta support
4. **`TaskActionItemPayload` missing 13+ fields**: `do`, `lt`, `icp`, `icc`, `icsd`, `sb`, `dl`, `lai`, `rmd`, `ato`, `acrd`, `xx`, `dds`
5. **`RepeaterConfiguration` missing fields**: `rrv`, `tp`, `ts`, `sr`
6. **No `Tombstone2` entity kind**: Dedicated deletion records with `dloid` and `dld` fields
7. **No `things-client-info` header**: Base64-encoded device metadata sent on requests
8. **No device registration**: `PUT /version/1/app-instance/{id}` for APNS registration
9. **`CheckListActionItemPayload` and `TagActionItemPayload` missing**: `lt`, `xx` fields

### Design Decisions

- **`TaskType` replaces `Boolean` for `tp`**: A new `TaskType` int enum (Task=0, Project=1, Heading=2). The `Task.IsProject` bool field becomes `Task.Type TaskType`. The `Boolean` type remains for other uses.
- **Structured notes**: A new `Note` struct handles the JSON format. `Task.Note string` stays for the aggregated plain-text value; parsing happens in state aggregation. `TaskActionItemPayload.Note` becomes `json.RawMessage` (already done on the branch).
- **Tombstone handling**: `Tombstone2` items trigger deletions in state aggregation — the `dloid` field tells us *which* object was deleted, and we delete it from the relevant map.
- **Debug logging gated**: The branch enables `httputil.DumpRequest/Response` unconditionally. We'll make it configurable via a `Debug` field on `Client`.
- **Backward compat for old entity versions**: The SDK already handles `Task3`, `Task4`, `Task6`, etc. We keep supporting all versions for reading old history streams.
- **`things-client-info` header**: Add a `ClientInfo` struct with sensible Mac defaults, base64-encode it, send on all requests.

---

### Task 1: Update User-Agent and add `things-client-info` header

**Files:**
- Modify: `client.go:54` (constant) and `client.go:56-83` (do method)

**Step 1: Write the failing test**

Create a test that verifies the User-Agent and `things-client-info` header are set correctly.

```go
// client_test.go
func TestClient_UserAgent(t *testing.T) {
	t.Parallel()
	var capturedUA string
	var capturedClientInfo string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		capturedClientInfo = r.Header.Get("things-client-info")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	c := New(ts.URL, "test@test.com", "password")
	req, _ := http.NewRequest("GET", "/test", nil)
	c.do(req)

	if capturedUA != "ThingsMac/32209501" {
		t.Errorf("expected User-Agent ThingsMac/32209501, got %s", capturedUA)
	}
	if capturedClientInfo == "" {
		t.Error("expected things-client-info header to be set")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestClient_UserAgent ./...`
Expected: FAIL — User-Agent mismatch (31516502 vs 32209501) and missing header

**Step 3: Write minimal implementation**

In `client.go`:
- Update `ThingsUserAgent` to `"ThingsMac/32209501"`
- Add a `ClientInfo` struct with JSON tags and base64 encoding
- Add default `ClientInfo` to `New()`
- Send `things-client-info` header in `do()`

```go
const ThingsUserAgent = "ThingsMac/32209501"

type ClientInfo struct {
	DeviceModel string `json:"dm"`
	LocalRegion string `json:"lr"`
	NF          bool   `json:"nf"`
	NK          bool   `json:"nk"`
	AppName     string `json:"nn"`
	AppVersion  string `json:"nv"`
	OSName      string `json:"on"`
	OSVersion   string `json:"ov"`
	PrimaryLang string `json:"pl"`
	UserLocale  string `json:"ul"`
}

func DefaultClientInfo() ClientInfo {
	return ClientInfo{
		DeviceModel: "MacBookPro18,3",
		LocalRegion: "US",
		NF:          true,
		NK:          true,
		AppName:     "ThingsMac",
		AppVersion:  "32209501",
		OSName:      "macOS",
		OSVersion:   "15.7.3",
		PrimaryLang: "en-US",
		UserLocale:  "en-Latn-US",
	}
}
```

In `Client` struct, add `ClientInfo ClientInfo` field, initialize in `New()`.

In `do()`, encode `ClientInfo` as base64 JSON and set `things-client-info` header.

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestClient_UserAgent ./...`
Expected: PASS

**Step 5: Make debug logging configurable**

Replace the unconditional `httputil.DumpRequest`/`DumpResponse` calls with a check on `c.Debug bool`:

```go
if c.Debug {
    bs, _ := httputil.DumpRequest(req, true)
    log.Println("REQUEST:", string(bs))
}
```

**Step 6: Run all tests**

Run: `go test -v ./...`
Expected: All PASS

**Step 7: Commit**

```bash
git add client.go client_test.go
git commit -m "feat: update User-Agent to 32209501, add things-client-info header, gate debug logging"
```

---

### Task 2: Replace `IsProject Boolean` with `TaskType` int enum

**Files:**
- Modify: `types.go` (add `TaskType`, update `Task` struct and `TaskActionItemPayload`)
- Modify: `state/memory/memory.go` (update `updateTask` and `Projects()`)
- Test: `types_test.go`, `state/memory/memory_test.go`

**Step 1: Write the failing test**

```go
// types_test.go
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
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestTaskType ./...`
Expected: FAIL — `TaskType` undefined

**Step 3: Write minimal implementation**

In `types.go`:

```go
// TaskType describes the type of a task entity
type TaskType int

const (
	TaskTypeTask    TaskType = 0
	TaskTypeProject TaskType = 1
	TaskTypeHeading TaskType = 2
)
```

Replace in `Task` struct:
```go
// Before:
IsProject bool
// After:
Type TaskType
```

Replace in `TaskActionItemPayload`:
```go
// Before:
IsProject *Boolean `json:"tp,omitempty"`
// After:
Type *TaskType `json:"tp,omitempty"`
```

Add helper:
```go
func TaskTypePtr(val TaskType) *TaskType {
	return &val
}
```

**Step 4: Update state/memory**

In `state/memory/memory.go`, `updateTask`:
```go
// Before:
if item.P.IsProject != nil {
    t.IsProject = bool(*item.P.IsProject)
}
// After:
if item.P.Type != nil {
    t.Type = *item.P.Type
}
```

In `Projects()`:
```go
// Before:
if !task.IsProject {
// After:
if task.Type != things.TaskTypeProject {
```

In `ProjectByName()`:
```go
// Before:
if !task.IsProject {
// After:
if task.Type != things.TaskTypeProject {
```

**Step 5: Run all tests**

Run: `go test -v ./...`
Expected: All PASS (some existing tests may need updating for the renamed field)

**Step 6: Commit**

```bash
git add types.go state/memory/memory.go helpers.go types_test.go
git commit -m "feat: replace IsProject boolean with TaskType enum (task/project/heading)"
```

---

### Task 3: Add missing fields to `TaskActionItemPayload` and `Task`

**Files:**
- Modify: `types.go` (both `Task` and `TaskActionItemPayload`)
- Modify: `state/memory/memory.go` (`updateTask`)
- Test: existing tests + new field-specific tests

**Step 1: Write the failing test**

```go
// types_test.go
func TestTaskActionItemPayload_AllFields(t *testing.T) {
	raw := `{
		"tt":"test","tp":0,"st":1,"ss":0,
		"sr":1770681600,"tir":1770681600,"dd":1771027200,"dds":null,
		"tr":false,"sp":null,"cd":1770713623.47,"md":1770713627.59,
		"ix":-346833,"ti":0,"do":0,"lt":false,"icp":false,"icc":0,
		"icsd":null,"sb":0,"ato":39600,"rmd":null,"acrd":null,
		"dl":[],"lai":null,
		"nt":{"_t":"tx","t":1,"ch":0,"v":""},
		"tg":[],"ar":[],"pr":[],"agr":[],"rt":[],"rr":null,
		"xx":{"sn":{},"_t":"oo"}
	}`
	var p TaskActionItemPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if p.DueOrder == nil || *p.DueOrder != 0 {
		t.Error("expected DueOrder=0")
	}
	if p.AlarmTimeOffset == nil || *p.AlarmTimeOffset != 39600 {
		t.Error("expected AlarmTimeOffset=39600")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestTaskActionItemPayload_AllFields ./...`
Expected: FAIL — missing fields

**Step 3: Add missing fields to `TaskActionItemPayload`**

```go
type TaskActionItemPayload struct {
	// ... existing fields ...
	DueOrder                *int            `json:"do,omitempty"`
	Leavable                *bool           `json:"lt,omitempty"`
	IsCompletedByChildren   *bool           `json:"icp,omitempty"`
	IsCompletedCount        *int            `json:"icc,omitempty"`
	InstanceCreationStartDate *Timestamp    `json:"icsd,omitempty"`
	SubtaskBehavior         *int            `json:"sb,omitempty"`
	DelegateIDs             *[]string       `json:"dl,omitempty"`
	LastActionItemID        *string         `json:"lai,omitempty"`
	ReminderDate            *Timestamp      `json:"rmd,omitempty"`
	AlarmTimeOffset         *int            `json:"ato,omitempty"`
	ActionRequiredDate      *Timestamp      `json:"acrd,omitempty"`
	DeadlineSuppression     *Timestamp      `json:"dds,omitempty"`
	ExtensionData           json.RawMessage `json:"xx,omitempty"`
}
```

Add corresponding fields to `Task` struct:
```go
type Task struct {
	// ... existing fields ...
	Type             TaskType
	TodayIndex       int
	DueOrder         int
	AlarmTimeOffset  *int
	TagIDs           []string
	RecurrenceIDs    []string
	DelegateIDs      []string
}
```

**Step 4: Update `state/memory/memory.go` `updateTask` for new fields**

Add field mappings for `AlarmTimeOffset`, `TagIDs`, `DueOrder`, `TodayIndex`, `DelegateIDs`, `RecurrenceIDs`.

**Step 5: Run all tests**

Run: `go test -v ./...`
Expected: All PASS

**Step 6: Commit**

```bash
git add types.go state/memory/memory.go types_test.go
git commit -m "feat: add missing task payload fields (alarm, delegates, due order, extension data, etc.)"
```

---

### Task 4: Add missing fields to `CheckListActionItemPayload` and `TagActionItemPayload`

**Files:**
- Modify: `types.go`

**Step 1: Write the failing test**

```go
// types_test.go
func TestCheckListActionItemPayload_ExtensionData(t *testing.T) {
	raw := `{"tt":"item","ix":0,"cd":1770713708.70,"md":1770713711.01,
		"ss":0,"sp":null,"lt":false,"ts":["abc"],"xx":{"sn":{},"_t":"oo"}}`
	var p CheckListActionItemPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if p.Leavable == nil || *p.Leavable != false {
		t.Error("expected Leavable=false")
	}
	if p.ExtensionData == nil {
		t.Error("expected ExtensionData to be set")
	}
}

func TestTagActionItemPayload_ExtensionData(t *testing.T) {
	raw := `{"tt":"tag","ix":0,"pn":[],"sh":null,"xx":{"sn":{},"_t":"oo"}}`
	var p TagActionItemPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if p.ExtensionData == nil {
		t.Error("expected ExtensionData to be set")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run "TestCheckListActionItemPayload_ExtensionData|TestTagActionItemPayload_ExtensionData" ./...`
Expected: FAIL

**Step 3: Add missing fields**

```go
type CheckListActionItemPayload struct {
	// ... existing ...
	Leavable      *bool           `json:"lt,omitempty"`
	ExtensionData json.RawMessage `json:"xx,omitempty"`
}

type TagActionItemPayload struct {
	// ... existing ...
	ExtensionData json.RawMessage `json:"xx,omitempty"`
}
```

**Step 4: Run all tests**

Run: `go test -v ./...`
Expected: All PASS

**Step 5: Commit**

```bash
git add types.go types_test.go
git commit -m "feat: add leavable and extension data fields to checklist and tag payloads"
```

---

### Task 5: Add `Tombstone2` entity kind and state handling

**Files:**
- Modify: `types.go` (add `ItemKindTombstone`, `TombstoneActionItemPayload`, `TombstoneActionItem`)
- Modify: `state/memory/memory.go` (handle `Tombstone2` in `Update`)
- Test: `state/memory/memory_test.go`

**Step 1: Write the failing test**

```go
// state/memory/memory_test.go
func TestState_Tombstone(t *testing.T) {
	s := NewState()
	// Create a task
	taskItem := things.Item{
		UUID:   "task-1",
		Kind:   things.ItemKindTask,
		Action: things.ItemActionCreated,
	}
	taskPayload, _ := json.Marshal(things.TaskActionItemPayload{
		Title: things.String("to be deleted"),
	})
	taskItem.P = taskPayload
	s.Update(taskItem)

	if _, ok := s.Tasks["task-1"]; !ok {
		t.Fatal("task should exist")
	}

	// Tombstone it
	tombItem := things.Item{
		UUID:   "tombstone-1",
		Kind:   things.ItemKindTombstone,
		Action: things.ItemActionCreated,
	}
	tombPayload, _ := json.Marshal(things.TombstoneActionItemPayload{
		DeletedObjectID: "task-1",
		DeletionDate:    1770713924.942709,
	})
	tombItem.P = tombPayload
	s.Update(tombItem)

	if _, ok := s.Tasks["task-1"]; ok {
		t.Error("task should have been deleted by tombstone")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestState_Tombstone ./state/memory/`
Expected: FAIL — undefined types

**Step 3: Add types in `types.go`**

```go
var (
	ItemKindTombstone  ItemKind = "Tombstone2"
)

type TombstoneActionItemPayload struct {
	DeletedObjectID string  `json:"dloid"`
	DeletionDate    float64 `json:"dld"`
}

type TombstoneActionItem struct {
	Item
	P TombstoneActionItemPayload `json:"p"`
}

func (t TombstoneActionItem) UUID() string {
	return t.Item.UUID
}
```

**Step 4: Handle in `state/memory/memory.go`**

Add case in `Update()`:

```go
case things.ItemKindTombstone:
	item := things.TombstoneActionItem{Item: rawItem}
	if err := json.Unmarshal(rawItem.P, &item.P); err != nil {
		continue
	}
	oid := item.P.DeletedObjectID
	delete(s.Tasks, oid)
	delete(s.Areas, oid)
	delete(s.Tags, oid)
	delete(s.CheckListItems, oid)
```

**Step 5: Run all tests**

Run: `go test -v ./...`
Expected: All PASS

**Step 6: Commit**

```bash
git add types.go state/memory/memory.go state/memory/memory_test.go
git commit -m "feat: add Tombstone2 entity kind for explicit deletion records"
```

---

### Task 6: Update `RepeaterConfiguration` with new fields

**Files:**
- Modify: `repeat.go`
- Test: `repeat_test.go`

**Step 1: Write the failing test**

```go
// repeat_test.go
func TestRepeaterConfiguration_NewFields(t *testing.T) {
	raw := `{
		"ia":1770681600,"rrv":4,
		"of":[{"wd":2}],"ts":0,"fu":256,
		"rc":0,"fa":1,"tp":1,"sr":1770681600,
		"ed":64092211200
	}`
	var rc RepeaterConfiguration
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if rc.Version != 4 {
		t.Errorf("expected Version=4, got %d", rc.Version)
	}
	if rc.Type != 1 {
		t.Errorf("expected Type=1, got %d", rc.Type)
	}
	if rc.TimeShift != 0 {
		t.Errorf("expected TimeShift=0, got %d", rc.TimeShift)
	}
	if rc.StartReference == nil {
		t.Error("expected StartReference to be set")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestRepeaterConfiguration_NewFields ./...`
Expected: FAIL

**Step 3: Add fields to `RepeaterConfiguration`**

```go
type RepeaterConfiguration struct {
	// ... existing fields ...
	Version        int        `json:"rrv,omitempty"`
	Type           int        `json:"tp,omitempty"`
	TimeShift      int        `json:"ts,omitempty"`
	StartReference *Timestamp `json:"sr,omitempty"`
}
```

**Step 4: Run all tests**

Run: `go test -v ./...`
Expected: All PASS

**Step 5: Commit**

```bash
git add repeat.go repeat_test.go
git commit -m "feat: add version, type, timeshift, start reference to RepeaterConfiguration"
```

---

### Task 7: Add device registration endpoint (`PUT /app-instance`)

**Files:**
- Create: `app_instance.go`
- Test: `app_instance_test.go`

**Step 1: Write the failing test**

```go
// app_instance_test.go
package thingscloud

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_RegisterAppInstance(t *testing.T) {
	t.Parallel()
	var capturedBody map[string]interface{}
	var capturedMethod string
	var capturedPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		bs, _ := ioutil.ReadAll(r.Body)
		json.Unmarshal(bs, &capturedBody)
		w.WriteHeader(200)
	}))
	defer ts.Close()

	c := New(ts.URL, "test@test.com", "password")
	err := c.RegisterAppInstance(AppInstanceRequest{
		AppInstanceID: "hash1-com.culturedcode.ThingsMac-hash2",
		HistoryKey:    "251943ab-63b5-45d1-8f9d-828a8d92fc15",
		APNSToken:     "token123",
		AppID:         "com.culturedcode.ThingsMac",
		Dev:           false,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedMethod != "PUT" {
		t.Errorf("expected PUT, got %s", capturedMethod)
	}
	if capturedPath != "/version/1/app-instance/hash1-com.culturedcode.ThingsMac-hash2" {
		t.Errorf("unexpected path: %s", capturedPath)
	}
	if capturedBody["history-key"] != "251943ab-63b5-45d1-8f9d-828a8d92fc15" {
		t.Error("expected history-key in body")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestClient_RegisterAppInstance ./...`
Expected: FAIL — undefined types/method

**Step 3: Write implementation**

```go
// app_instance.go
package thingscloud

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type AppInstanceRequest struct {
	AppInstanceID string `json:"-"`
	HistoryKey    string `json:"history-key"`
	APNSToken     string `json:"apns-token"`
	AppID         string `json:"app-id"`
	Dev           bool   `json:"dev"`
}

func (c *Client) RegisterAppInstance(req AppInstanceRequest) error {
	bs, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequest("PUT",
		fmt.Sprintf("/version/1/app-instance/%s", req.AppInstanceID), bytes.NewReader(bs))
	if err != nil {
		return err
	}
	resp, err := c.do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http response code: %s", resp.Status)
	}
	return nil
}
```

**Step 4: Run all tests**

Run: `go test -v ./...`
Expected: All PASS

**Step 5: Commit**

```bash
git add app_instance.go app_instance_test.go
git commit -m "feat: add PUT /app-instance device registration endpoint"
```

---

### Task 8: Add structured `Note` type with full-text and delta support

**Files:**
- Create: `notes.go`
- Test: `notes_test.go`
- Modify: `state/memory/memory.go` (update note parsing in `updateTask`)

**Step 1: Write the failing test**

```go
// notes_test.go
package thingscloud

import (
	"encoding/json"
	"testing"
)

func TestNote_FullText(t *testing.T) {
	raw := `{"_t":"tx","t":1,"ch":0,"v":"Hello world"}`
	var n Note
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatal(err)
	}
	if n.Type != NoteTypeFullText {
		t.Errorf("expected type %d, got %d", NoteTypeFullText, n.Type)
	}
	if n.Value != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", n.Value)
	}
}

func TestNote_Delta(t *testing.T) {
	raw := `{"_t":"tx","t":2,"ps":[{"r":"inserted text","p":0,"l":0,"ch":12345}]}`
	var n Note
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatal(err)
	}
	if n.Type != NoteTypeDelta {
		t.Errorf("expected type %d, got %d", NoteTypeDelta, n.Type)
	}
	if len(n.Patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(n.Patches))
	}
	if n.Patches[0].Replacement != "inserted text" {
		t.Errorf("unexpected replacement: %s", n.Patches[0].Replacement)
	}
}

func TestNote_ApplyPatch(t *testing.T) {
	original := "Hello world"
	patch := NotePatch{Position: 5, Length: 6, Replacement: " Go"}
	result := ApplyPatches(original, []NotePatch{patch})
	if result != "Hello Go" {
		t.Errorf("expected 'Hello Go', got '%s'", result)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestNote ./...`
Expected: FAIL

**Step 3: Write implementation**

```go
// notes.go
package thingscloud

const (
	NoteTypeFullText = 1
	NoteTypeDelta    = 2
)

type NotePatch struct {
	Replacement string `json:"r"`
	Position    int    `json:"p"`
	Length      int    `json:"l"`
	Checksum    int64  `json:"ch"`
}

type Note struct {
	TypeTag  string      `json:"_t"`
	Type     int         `json:"t"`
	Checksum int64       `json:"ch,omitempty"`
	Value    string      `json:"v,omitempty"`
	Patches  []NotePatch `json:"ps,omitempty"`
}

func ApplyPatches(original string, patches []NotePatch) string {
	runes := []rune(original)
	for _, p := range patches {
		end := p.Position + p.Length
		if end > len(runes) {
			end = len(runes)
		}
		result := make([]rune, 0, len(runes)-p.Length+len([]rune(p.Replacement)))
		result = append(result, runes[:p.Position]...)
		result = append(result, []rune(p.Replacement)...)
		result = append(result, runes[end:]...)
		runes = result
	}
	return string(runes)
}
```

**Step 4: Update `state/memory/memory.go` note parsing**

Replace the existing note parsing block in `updateTask` with:

```go
if item.P.Note != nil {
	var noteStr string
	if err := json.Unmarshal(item.P.Note, &noteStr); err == nil {
		t.Note = noteStr
	} else {
		var note things.Note
		if err := json.Unmarshal(item.P.Note, &note); err == nil {
			switch note.Type {
			case things.NoteTypeFullText:
				t.Note = note.Value
			case things.NoteTypeDelta:
				t.Note = things.ApplyPatches(t.Note, note.Patches)
			}
		}
	}
}
```

**Step 5: Run all tests**

Run: `go test -v ./...`
Expected: All PASS

**Step 6: Commit**

```bash
git add notes.go notes_test.go state/memory/memory.go
git commit -m "feat: add structured Note type with full-text and delta patch support"
```

---

### Task 9: Add `Headings()` and `TasksByHeading()` to state

**Files:**
- Modify: `state/memory/memory.go`
- Test: `state/memory/memory_test.go`

**Step 1: Write the failing test**

```go
// state/memory/memory_test.go
func TestState_Headings(t *testing.T) {
	s := NewState()
	// Create a project
	projItem := things.Item{UUID: "proj-1", Kind: things.ItemKindTask, Action: things.ItemActionCreated}
	projPayload, _ := json.Marshal(things.TaskActionItemPayload{
		Title: things.String("My Project"),
		Type:  things.TaskTypePtr(things.TaskTypeProject),
	})
	projItem.P = projPayload

	// Create a heading under the project
	headItem := things.Item{UUID: "head-1", Kind: things.ItemKindTask, Action: things.ItemActionCreated}
	headPayload, _ := json.Marshal(things.TaskActionItemPayload{
		Title:          things.String("Phase 1"),
		Type:           things.TaskTypePtr(things.TaskTypeHeading),
		ParentTaskIDs:  &[]string{"proj-1"},
	})
	headItem.P = headPayload

	s.Update(projItem, headItem)

	headings := s.Headings("proj-1")
	if len(headings) != 1 {
		t.Fatalf("expected 1 heading, got %d", len(headings))
	}
	if headings[0].Title != "Phase 1" {
		t.Errorf("expected 'Phase 1', got '%s'", headings[0].Title)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestState_Headings ./state/memory/`
Expected: FAIL

**Step 3: Implement `Headings()` and `TasksByHeading()`**

```go
// Headings returns all headings within a project
func (s *State) Headings(projectID string) []*things.Task {
	tasks := []*things.Task{}
	for _, task := range s.Tasks {
		if task.Type != things.TaskTypeHeading {
			continue
		}
		for _, pid := range task.ParentTaskIDs {
			if pid == projectID {
				tasks = append(tasks, task)
				break
			}
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Index < tasks[j].Index
	})
	return tasks
}

// TasksByHeading returns tasks assigned to a specific heading (action group)
func (s *State) TasksByHeading(headingID string, opts ListOption) []*things.Task {
	tasks := []*things.Task{}
	for _, task := range s.Tasks {
		if task.Type != things.TaskTypeTask {
			continue
		}
		if task.Status == things.TaskStatusCompleted && opts.ExcludeCompleted {
			continue
		}
		if task.InTrash && opts.ExcludeInTrash {
			continue
		}
		for _, agr := range task.ActionGroupIDs {
			if agr == headingID {
				tasks = append(tasks, task)
				break
			}
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Index < tasks[j].Index
	})
	return tasks
}
```

**Step 4: Run all tests**

Run: `go test -v ./...`
Expected: All PASS

**Step 5: Commit**

```bash
git add state/memory/memory.go state/memory/memory_test.go
git commit -m "feat: add Headings() and TasksByHeading() to state queries"
```

---

### Task 10: Final cleanup and full test pass

**Files:**
- Modify: various (fix any remaining compilation issues, update examples)
- Remove: `things-cli` binary from repo (committed binary on branch)

**Step 1: Run full build**

Run: `go build -v ./...`
Expected: Clean build

**Step 2: Run full test suite**

Run: `go test -v ./...`
Expected: All PASS

**Step 3: Run go vet**

Run: `go vet ./...`
Expected: No issues

**Step 4: Clean up committed binary**

```bash
git rm things-cli
```

**Step 5: Final commit**

```bash
git add -A
git commit -m "chore: final cleanup, remove committed binary, verify all tests pass"
```
