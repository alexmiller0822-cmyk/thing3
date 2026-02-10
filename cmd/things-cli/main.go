package main

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	thingscloud "github.com/nicolai86/things-cloud-sdk"
	memory "github.com/nicolai86/things-cloud-sdk/state/memory"
)

// ---------------------------------------------------------------------------
// Wire-format types (no omitempty — Things expects all fields on creates)
// ---------------------------------------------------------------------------

// WireNote matches the Things note wire format exactly.
// Field order must be _t, ch, v, t (matching what Things.app expects).
type WireNote struct {
	TypeTag  string `json:"_t"`
	Checksum int64  `json:"ch"`
	Value    string `json:"v"`
	Type     int    `json:"t"`
}

// WireExtension is the required xx field: {sn: {}, _t: "oo"}.
type WireExtension struct {
	Sn      map[string]any `json:"sn"`
	TypeTag string         `json:"_t"`
}

// writeEnvelope is a single generic wrapper for history.Write().
// Implements Identifiable (UUID()) and json.Marshaler.
type writeEnvelope struct {
	id      string
	action  int
	kind    string
	payload any
}

func (w writeEnvelope) UUID() string { return w.id }

func (w writeEnvelope) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		T int    `json:"t"`
		E string `json:"e"`
		P any    `json:"p"`
	}{w.action, w.kind, w.payload})
}

// TaskCreatePayload — all 34 fields, no omitempty, field order matches HAR.
type TaskCreatePayload struct {
	Tp   int              `json:"tp"`
	Sr   *int64           `json:"sr"`
	Dds  *int64           `json:"dds"`
	Rt   []string         `json:"rt"`
	Rmd  *int64           `json:"rmd"`
	Ss   int              `json:"ss"`
	Tr   bool             `json:"tr"`
	Dl   []string         `json:"dl"`
	Icp  bool             `json:"icp"`
	St   int              `json:"st"`
	Ar   []string         `json:"ar"`
	Tt   string           `json:"tt"`
	Do   int              `json:"do"`
	Lai  *int64           `json:"lai"`
	Tir  *int64           `json:"tir"`
	Tg   []string         `json:"tg"`
	Agr  []string         `json:"agr"`
	Ix   int              `json:"ix"`
	Cd   float64          `json:"cd"`
	Lt   bool             `json:"lt"`
	Icc  int              `json:"icc"`
	Md   *float64         `json:"md"`
	Ti   int              `json:"ti"`
	Dd   *int64           `json:"dd"`
	Ato  *int             `json:"ato"`
	Nt   WireNote         `json:"nt"`
	Icsd *int64           `json:"icsd"`
	Pr   []string         `json:"pr"`
	Rp   *string          `json:"rp"`
	Acrd *int64           `json:"acrd"`
	Sp   *float64         `json:"sp"`
	Sb   int              `json:"sb"`
	Rr   *json.RawMessage `json:"rr"`
	Xx   WireExtension    `json:"xx"`
}

// ChecklistItemCreatePayload — all 9 fields for checklist item creation.
type ChecklistItemCreatePayload struct {
	Cd   float64       `json:"cd"`
	Md   *float64      `json:"md"`
	Tt   string        `json:"tt"`
	Ss   int           `json:"ss"`
	Sp   *float64      `json:"sp"`
	Ix   int           `json:"ix"`
	Ts   []string      `json:"ts"`
	Lt   bool          `json:"lt"`
	Xx   WireExtension `json:"xx"`
}

// TagCreatePayload — all 5 fields for tag creation.
type TagCreatePayload struct {
	Tt string        `json:"tt"`
	Ix int           `json:"ix"`
	Sh *string       `json:"sh"`
	Pn []string      `json:"pn"`
	Xx WireExtension `json:"xx"`
}

// ---------------------------------------------------------------------------
// Helpers: notes, UUID, timestamps, errors
// ---------------------------------------------------------------------------

func emptyNote() WireNote {
	return WireNote{TypeTag: "tx", Checksum: 0, Value: "", Type: 1}
}

func noteChecksum(s string) int64 {
	return int64(crc32.ChecksumIEEE([]byte(s)))
}

func textNote(s string) WireNote {
	return WireNote{TypeTag: "tx", Checksum: noteChecksum(s), Value: s, Type: 1}
}

// WireNotePatch represents a note in patch/delta format (type 2), as used by iOS.
// This is the format Things expects for note updates.
type WireNotePatch struct {
	Type    int             `json:"t"`
	Patches []NotePatchItem `json:"ps"`
	TypeTag string          `json:"_t"`
}

type NotePatchItem struct {
	Replacement string `json:"r"`
	Position    int    `json:"p"`
	Length      int    `json:"l"`
	Checksum    int64  `json:"ch"`
}

func patchNote(s string) WireNotePatch {
	return WireNotePatch{
		Type: 2,
		Patches: []NotePatchItem{
			{Replacement: s, Position: 0, Length: 0, Checksum: noteChecksum(s)},
		},
		TypeTag: "tx",
	}
}

func defaultExtension() WireExtension {
	return WireExtension{Sn: map[string]any{}, TypeTag: "oo"}
}

func generateUUID() string {
	u := uuid.New()
	// Base58 alphabet (Bitcoin/Flickr): no 0, O, I, l
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	n := new(big.Int).SetBytes(u[:])
	base := big.NewInt(58)
	mod := new(big.Int)
	var encoded []byte
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		encoded = append(encoded, alphabet[mod.Int64()])
	}
	// Reverse (big-endian)
	for i, j := 0, len(encoded)-1; i < j; i, j = i+1, j-1 {
		encoded[i], encoded[j] = encoded[j], encoded[i]
	}
	return string(encoded)
}

func nowTs() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

func todayMidnightUTC() int64 {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Unix()
}

func parseDate(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

func parseArgs(args []string) map[string]string {
	result := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			key := strings.TrimPrefix(args[i], "--")
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				result[key] = args[i+1]
				i++
			} else {
				result[key] = "true"
			}
		}
	}
	return result
}

func fatal(op string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", op, err)
	os.Exit(1)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func requireArgs(args []string, min int, usage string) {
	if len(args) < min {
		fatalf("Usage: %s", usage)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fatalf("%s is required", key)
	}
	return v
}

func outputJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// ---------------------------------------------------------------------------
// Payload builders
// ---------------------------------------------------------------------------

// newBlankTaskSkeleton creates a completely empty task skeleton matching iOS's
// create format. All real data is added via a subsequent UPDATE commit.
func newBlankTaskSkeleton() TaskCreatePayload {
	now := nowTs()
	return TaskCreatePayload{
		Tp:   0,
		Sr:   nil,
		Dds:  nil,
		Rt:   []string{},
		Rmd:  nil,
		Ss:   0,
		Tr:   false,
		Dl:   []string{},
		Icp:  false,
		St:   0,
		Ar:   []string{},
		Tt:   "",   // empty title — set via update
		Do:   0,
		Lai:  nil,
		Tir:  nil,
		Tg:   []string{},
		Agr:  []string{},
		Ix:   0,
		Cd:   now,
		Lt:   false,
		Icc:  0,
		Md:   nil, // null on create, set via update
		Ti:   0,
		Dd:   nil,
		Ato:  nil,
		Nt:   emptyNote(),
		Icsd: nil,
		Pr:   []string{},
		Rp:   nil,
		Acrd: nil,
		Sp:   nil,
		Sb:   0,
		Rr:   nil,
		Xx:   defaultExtension(),
	}
}

func newTaskCreatePayload(title string, opts map[string]string) TaskCreatePayload {
	now := nowTs()

	// Defaults
	var st int
	var sr *int64
	var tir *int64
	var dd *int64
	tp := 0
	pr := []string{}
	agr := []string{}
	ar := []string{}
	tg := []string{}
	nt := emptyNote()

	// --type
	if v, ok := opts["type"]; ok {
		switch v {
		case "project":
			tp = 1
		case "heading":
			tp = 2
		}
	}

	// --when (schedule mapping per HAR)
	if v, ok := opts["when"]; ok {
		switch v {
		case "today":
			st = 1
			today := todayMidnightUTC()
			sr = &today
			tir = &today
		case "anytime":
			st = 1
		case "someday":
			st = 2
		case "inbox":
			st = 0
		}
	}

	// --note
	if v, ok := opts["note"]; ok && v != "" {
		nt = textNote(v)
	}

	// --deadline
	if v, ok := opts["deadline"]; ok {
		if t := parseDate(v); t != nil {
			ts := t.Unix()
			dd = &ts
		}
	}

	// --scheduled (overrides sr/tir; sets st=1 with dates if not already set by --when)
	if v, ok := opts["scheduled"]; ok {
		if t := parseDate(v); t != nil {
			ts := t.Unix()
			sr = &ts
			tir = &ts
			if _, hasWhen := opts["when"]; !hasWhen {
				st = 1 // default to anytime+date
			}
		}
	}

	// --project
	if v, ok := opts["project"]; ok && v != "" {
		pr = []string{v}
	}

	// --heading
	if v, ok := opts["heading"]; ok && v != "" {
		agr = []string{v}
	}

	// --area
	if v, ok := opts["area"]; ok && v != "" {
		ar = []string{v}
	}

	// --tags (comma-separated)
	if v, ok := opts["tags"]; ok && v != "" {
		tg = strings.Split(v, ",")
	}

	// --uuid handled by caller

	return TaskCreatePayload{
		Tp:   tp,
		Sr:   sr,
		Dds:  nil,
		Rt:   []string{},
		Rmd:  nil,
		Ss:   0,
		Tr:   false,
		Dl:   []string{},
		Icp:  false,
		St:   st,
		Ar:   ar,
		Tt:   title,
		Do:   0,
		Lai:  nil,
		Tir:  tir,
		Tg:   tg,
		Agr:  agr,
		Ix:   0,
		Cd:   now,
		Lt:   false,
		Icc:  0,
		Md:   nil, // must be null for creates — Things.app crashes otherwise
		Ti:   0,
		Dd:   dd,
		Ato:  nil,
		Nt:   nt,
		Icsd: nil,
		Pr:   pr,
		Rp:   nil,
		Acrd: nil,
		Sp:   nil,
		Sb:   0,
		Rr:   nil,
		Xx:   defaultExtension(),
	}
}

// ---------------------------------------------------------------------------
// Fluent update builder — for sparse updates (edit, complete, trash, etc.)
// ---------------------------------------------------------------------------

type taskUpdate struct {
	fields map[string]any
}

func newTaskUpdate() *taskUpdate {
	return &taskUpdate{fields: map[string]any{
		"md": nowTs(),
	}}
}

func (u *taskUpdate) Title(s string) *taskUpdate {
	u.fields["tt"] = s
	return u
}

func (u *taskUpdate) Note(text string) *taskUpdate {
	u.fields["nt"] = textNote(text)
	return u
}

func (u *taskUpdate) NotePatch(text string) *taskUpdate {
	u.fields["nt"] = patchNote(text)
	return u
}

func (u *taskUpdate) ClearNote() *taskUpdate {
	u.fields["nt"] = emptyNote()
	return u
}

func (u *taskUpdate) Status(ss int) *taskUpdate {
	u.fields["ss"] = ss
	return u
}

func (u *taskUpdate) StopDate(ts float64) *taskUpdate {
	u.fields["sp"] = ts
	return u
}

func (u *taskUpdate) Trash(b bool) *taskUpdate {
	u.fields["tr"] = b
	return u
}

func (u *taskUpdate) Schedule(st int, sr, tir any) *taskUpdate {
	u.fields["st"] = st
	u.fields["sr"] = sr
	u.fields["tir"] = tir
	return u
}

func (u *taskUpdate) Deadline(dd int64) *taskUpdate {
	u.fields["dd"] = dd
	return u
}

func (u *taskUpdate) Scheduled(sr, tir int64) *taskUpdate {
	u.fields["sr"] = sr
	u.fields["tir"] = tir
	return u
}

func (u *taskUpdate) build() map[string]any {
	return u.fields
}

// ---------------------------------------------------------------------------
// cliContext and state loading
// ---------------------------------------------------------------------------

type cliContext struct {
	client  *thingscloud.Client
	history *thingscloud.History
}

func initCLI() *cliContext {
	username := requireEnv("THINGS_USERNAME")
	password := requireEnv("THINGS_PASSWORD")

	c := thingscloud.New(thingscloud.APIEndpoint, username, password)
	if os.Getenv("THINGS_DEBUG") != "" {
		c.Debug = true
	}

	if _, err := c.Verify(); err != nil {
		fatal("login", err)
	}

	history, err := c.OwnHistory()
	if err != nil {
		fatal("get history", err)
	}
	if err := history.Sync(); err != nil {
		fatal("sync history", err)
	}

	return &cliContext{client: c, history: history}
}

func (ctx *cliContext) loadState() *memory.State {
	var allItems []thingscloud.Item
	startIndex := 0
	for {
		items, hasMore, err := ctx.history.Items(thingscloud.ItemsOptions{StartIndex: startIndex})
		if err != nil {
			fatal("fetch items", err)
		}
		allItems = append(allItems, items...)
		if !hasMore {
			break
		}
		startIndex = ctx.history.LoadedServerIndex
	}

	state := memory.NewState()
	state.Update(allItems...)
	return state
}

// ---------------------------------------------------------------------------
// Read commands
// ---------------------------------------------------------------------------

// TaskOutput is a JSON-friendly task representation for CLI output.
type TaskOutput struct {
	UUID          string   `json:"uuid"`
	Title         string   `json:"title"`
	Note          string   `json:"note,omitempty"`
	Status        int      `json:"status"`
	InTrash       bool     `json:"inTrash"`
	IsProject     bool     `json:"isProject"`
	Schedule      int      `json:"schedule"`
	ScheduledDate *string  `json:"scheduledDate,omitempty"`
	DeadlineDate  *string  `json:"deadlineDate,omitempty"`
	AreaIDs       []string `json:"areaIds,omitempty"`
	ParentIDs     []string `json:"parentIds,omitempty"`
}

func taskToOutput(t *thingscloud.Task) TaskOutput {
	out := TaskOutput{
		UUID:      t.UUID,
		Title:     t.Title,
		Note:      t.Note,
		Status:    int(t.Status),
		InTrash:   t.InTrash,
		IsProject: t.Type == thingscloud.TaskTypeProject,
		Schedule:  int(t.Schedule),
		AreaIDs:   t.AreaIDs,
		ParentIDs: t.ParentTaskIDs,
	}
	if t.ScheduledDate != nil {
		s := t.ScheduledDate.Format("2006-01-02")
		out.ScheduledDate = &s
	}
	if t.DeadlineDate != nil {
		s := t.DeadlineDate.Format("2006-01-02")
		out.DeadlineDate = &s
	}
	return out
}

func cmdList(state *memory.State, args []string) {
	opts := parseArgs(args)
	todayStart := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 0, 0, 0, 0, time.UTC)

	var tasks []TaskOutput
	for _, task := range state.Tasks {
		if task.InTrash || task.Status == 3 || task.Type == thingscloud.TaskTypeProject {
			continue
		}

		// Filters
		if _, ok := opts["today"]; ok {
			if task.Schedule != thingscloud.TaskScheduleAnytime || task.ScheduledDate == nil || !task.ScheduledDate.Equal(todayStart) {
				continue
			}
		}
		if _, ok := opts["inbox"]; ok {
			if task.Schedule != thingscloud.TaskScheduleInbox {
				continue
			}
		}
		if areaName, ok := opts["area"]; ok {
			areaUUID := findAreaUUID(state, areaName)
			if !containsStr(task.AreaIDs, areaUUID) {
				continue
			}
		}
		if projectName, ok := opts["project"]; ok {
			projectUUID := findProjectUUID(state, projectName)
			if !containsStr(task.ActionGroupIDs, projectUUID) {
				continue
			}
		}

		tasks = append(tasks, taskToOutput(task))
	}
	outputJSON(tasks)
}

func cmdShow(state *memory.State, uuid string) {
	for _, task := range state.Tasks {
		if strings.HasPrefix(task.UUID, uuid) {
			outputJSON(taskToOutput(task))
			return
		}
	}
	fatalf("task not found: %s", uuid)
}

func cmdAreas(state *memory.State) {
	type AreaOutput struct {
		UUID  string `json:"uuid"`
		Title string `json:"title"`
	}
	var areas []AreaOutput
	for _, area := range state.Areas {
		areas = append(areas, AreaOutput{UUID: area.UUID, Title: area.Title})
	}
	outputJSON(areas)
}

func cmdProjects(state *memory.State) {
	var projects []TaskOutput
	for _, task := range state.Tasks {
		if task.Type == thingscloud.TaskTypeProject && !task.InTrash && task.Status != 3 {
			projects = append(projects, taskToOutput(task))
		}
	}
	outputJSON(projects)
}

func cmdTags(state *memory.State) {
	type TagOutput struct {
		UUID      string   `json:"uuid"`
		Title     string   `json:"title"`
		Shorthand string   `json:"shorthand,omitempty"`
		ParentIDs []string `json:"parentIds,omitempty"`
	}
	var tags []TagOutput
	for _, tag := range state.Tags {
		tags = append(tags, TagOutput{
			UUID:      tag.UUID,
			Title:     tag.Title,
			Shorthand: tag.ShortHand,
			ParentIDs: tag.ParentTagIDs,
		})
	}
	outputJSON(tags)
}

// helpers for cmdList filters
func findAreaUUID(state *memory.State, name string) string {
	for _, area := range state.Areas {
		if strings.EqualFold(area.Title, name) {
			return area.UUID
		}
	}
	fatalf("area not found: %s", name)
	return ""
}

func findProjectUUID(state *memory.State, name string) string {
	for _, task := range state.Tasks {
		if task.Type == thingscloud.TaskTypeProject && strings.EqualFold(task.Title, name) {
			return task.UUID
		}
	}
	fatalf("project not found: %s", name)
	return ""
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Write commands
// ---------------------------------------------------------------------------

func cmdCreate(history *thingscloud.History, args []string) {
	requireArgs(args, 1, "things-cli create \"Title\" [--note ...] [--when today|anytime|someday|inbox] [--deadline YYYY-MM-DD] [--scheduled YYYY-MM-DD] [--project UUID] [--heading UUID] [--area UUID] [--tags UUID,...] [--type task|project|heading] [--uuid UUID]")

	title := args[0]
	opts := parseArgs(args[1:])

	taskUUID := opts["uuid"]
	if taskUUID == "" {
		taskUUID = generateUUID()
	}

	// Extract note — handled separately via two-commit pattern if present.
	noteText := opts["note"]
	delete(opts, "note")

	// Single-commit create with all data (no note).
	// Matches iOS's format for no-note tasks.
	payload := newTaskCreatePayload(title, opts)
	env := writeEnvelope{id: taskUUID, action: 0, kind: "Task6", payload: payload}
	if err := history.Write(env); err != nil {
		fatal("create task", err)
	}

	// If note requested, add via separate update commit using patch format.
	// iOS uses this two-commit pattern: blank skeleton create, then update with note.
	// Things.app crashes on INSERT of new tasks with non-empty notes from cloud.
	if noteText != "" {
		u := newTaskUpdate().NotePatch(noteText)
		editEnv := writeEnvelope{id: taskUUID, action: 1, kind: "Task6", payload: u.build()}
		if err := history.Write(editEnv); err != nil {
			fatal("add note to task", err)
		}
	}

	outputJSON(map[string]string{"status": "created", "uuid": taskUUID, "title": title})
}

func cmdEdit(history *thingscloud.History, taskUUID string, args []string) {
	opts := parseArgs(args)
	if len(opts) == 0 {
		fatalf("Usage: things-cli edit <uuid> [--title ...] [--note ...] [--when today|anytime|someday|inbox] [--deadline YYYY-MM-DD] [--scheduled YYYY-MM-DD]")
	}

	u := newTaskUpdate()

	if v, ok := opts["title"]; ok {
		u.Title(v)
	}
	if v, ok := opts["note"]; ok {
		if v == "" {
			u.ClearNote()
		} else {
			u.Note(v)
		}
	}
	if v, ok := opts["when"]; ok {
		switch v {
		case "today":
			today := todayMidnightUTC()
			u.Schedule(1, today, today)
		case "anytime":
			u.Schedule(1, nil, nil)
		case "someday":
			u.Schedule(2, nil, nil)
		case "inbox":
			u.Schedule(0, nil, nil)
		}
	}
	if v, ok := opts["deadline"]; ok {
		if t := parseDate(v); t != nil {
			u.Deadline(t.Unix())
		}
	}
	if v, ok := opts["scheduled"]; ok {
		if t := parseDate(v); t != nil {
			ts := t.Unix()
			u.Scheduled(ts, ts)
			if _, hasWhen := opts["when"]; !hasWhen {
				u.Schedule(1, ts, ts)
			}
		}
	}

	env := writeEnvelope{id: taskUUID, action: 1, kind: "Task6", payload: u.build()}
	if err := history.Write(env); err != nil {
		fatal("edit task", err)
	}

	outputJSON(map[string]string{"status": "updated", "uuid": taskUUID})
}

func cmdComplete(history *thingscloud.History, taskUUID string) {
	ts := nowTs()
	u := newTaskUpdate().Status(3).StopDate(ts)

	env := writeEnvelope{id: taskUUID, action: 1, kind: "Task6", payload: u.build()}
	if err := history.Write(env); err != nil {
		fatal("complete task", err)
	}

	outputJSON(map[string]string{"status": "completed", "uuid": taskUUID})
}

func cmdTrash(history *thingscloud.History, taskUUID string) {
	u := newTaskUpdate().Trash(true)

	env := writeEnvelope{id: taskUUID, action: 1, kind: "Task6", payload: u.build()}
	if err := history.Write(env); err != nil {
		fatal("trash task", err)
	}

	outputJSON(map[string]string{"status": "trashed", "uuid": taskUUID})
}

func cmdPurge(history *thingscloud.History, taskUUID string) {
	tombstoneUUID := generateUUID()
	payload := map[string]any{
		"dloid": taskUUID,
		"dld":   nowTs(),
	}

	env := writeEnvelope{id: tombstoneUUID, action: 0, kind: "Tombstone2", payload: payload}
	if err := history.Write(env); err != nil {
		fatal("purge task", err)
	}

	outputJSON(map[string]string{"status": "purged", "uuid": taskUUID})
}

func cmdMoveToToday(history *thingscloud.History, taskUUID string) {
	today := todayMidnightUTC()
	u := newTaskUpdate().Schedule(1, today, today)

	env := writeEnvelope{id: taskUUID, action: 1, kind: "Task6", payload: u.build()}
	if err := history.Write(env); err != nil {
		fatal("move to today", err)
	}

	outputJSON(map[string]string{"status": "moved-to-today", "uuid": taskUUID})
}

func cmdCreateTag(history *thingscloud.History, args []string) {
	requireArgs(args, 1, `things-cli create-tag "Name" [--shorthand KEY] [--parent UUID]`)

	title := args[0]
	opts := parseArgs(args[1:])

	tagUUID := opts["uuid"]
	if tagUUID == "" {
		tagUUID = generateUUID()
	}

	// Per HAR: ix is negative, sh is null on create, pn is []
	var sh *string
	if v, ok := opts["shorthand"]; ok {
		sh = &v
	}

	pn := []string{}
	if v, ok := opts["parent"]; ok && v != "" {
		pn = []string{v}
	}

	payload := TagCreatePayload{
		Tt: title,
		Ix: -1237, // Things uses negative indices
		Sh: sh,
		Pn: pn,
		Xx: defaultExtension(),
	}

	env := writeEnvelope{id: tagUUID, action: 0, kind: "Tag4", payload: payload}
	if err := history.Write(env); err != nil {
		fatal("create tag", err)
	}

	outputJSON(map[string]string{"status": "created", "uuid": tagUUID, "title": title})
}

// ---------------------------------------------------------------------------
// main() dispatch and usage
// ---------------------------------------------------------------------------

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: things-cli <command> [args]

Read commands (load state from cloud):
  list [--today] [--inbox] [--area NAME] [--project NAME]
  show <uuid>
  areas
  projects
  tags

Write commands (fast — skip state loading):
  create "Title" [--note ...] [--when today|anytime|someday|inbox]
         [--deadline YYYY-MM-DD] [--scheduled YYYY-MM-DD]
         [--project UUID] [--heading UUID] [--area UUID]
         [--tags UUID,...] [--type task|project|heading] [--uuid UUID]
  create-tag "Name" [--shorthand KEY] [--parent UUID]
  edit <uuid> [--title ...] [--note ...] [--when ...] [--deadline ...] [--scheduled ...]
  complete <uuid>
  trash <uuid>
  purge <uuid>
  move-to-today <uuid>`)
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx := initCLI()
	cmd := os.Args[1]

	switch cmd {
	// Read commands — need state
	case "list":
		cmdList(ctx.loadState(), os.Args[2:])
	case "show":
		requireArgs(os.Args[2:], 1, "things-cli show <uuid>")
		cmdShow(ctx.loadState(), os.Args[2])
	case "areas":
		cmdAreas(ctx.loadState())
	case "projects":
		cmdProjects(ctx.loadState())
	case "tags":
		cmdTags(ctx.loadState())

	// Write commands — skip state loading
	case "create":
		cmdCreate(ctx.history, os.Args[2:])
	case "create-tag":
		cmdCreateTag(ctx.history, os.Args[2:])
	case "edit":
		requireArgs(os.Args[2:], 1, "things-cli edit <uuid> [--title ...] [--note ...]")
		cmdEdit(ctx.history, os.Args[2], os.Args[3:])
	case "complete":
		requireArgs(os.Args[2:], 1, "things-cli complete <uuid>")
		cmdComplete(ctx.history, os.Args[2])
	case "trash":
		requireArgs(os.Args[2:], 1, "things-cli trash <uuid>")
		cmdTrash(ctx.history, os.Args[2])
	case "purge":
		requireArgs(os.Args[2:], 1, "things-cli purge <uuid>")
		cmdPurge(ctx.history, os.Args[2])
	case "move-to-today":
		requireArgs(os.Args[2:], 1, "things-cli move-to-today <uuid>")
		cmdMoveToToday(ctx.history, os.Args[2])

	default:
		fatalf("unknown command: %s", cmd)
	}
}
