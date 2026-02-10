package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	thingscloud "github.com/nicolai86/things-cloud-sdk"
	memory "github.com/nicolai86/things-cloud-sdk/state/memory"
)

type TaskOutput struct {
	UUID          string  `json:"uuid"`
	Title         string  `json:"title"`
	Note          string  `json:"note,omitempty"`
	Status        int     `json:"status"`
	InTrash       bool    `json:"inTrash"`
	IsProject     bool    `json:"isProject"`
	Schedule      int     `json:"schedule"`
	ScheduledDate *string `json:"scheduledDate,omitempty"`
	DeadlineDate  *string `json:"deadlineDate,omitempty"`
	AreaIDs       []string `json:"areaIds,omitempty"`
	ParentIDs     []string `json:"parentIds,omitempty"`
}

// ThingsNote represents the note format Things expects
// IMPORTANT: Field order must match Things exactly: _t, ch, v, t
type ThingsNote struct {
	Typ string      `json:"_t"`           // always "tx" - MUST be first
	Ch  int64       `json:"ch"`           // checksum
	V   string      `json:"v"`            // value for simple notes (MUST include even if empty)
	T   int         `json:"t"`            // 1 = empty/simple, 2 = with patches - MUST be last
	Ps  []NotePatch `json:"ps,omitempty"` // patches for complex notes
}

type NotePatch struct {
	R  string `json:"r"`  // replacement text
	P  int    `json:"p"`  // position
	L  int    `json:"l"`  // length to replace
	Ch int64  `json:"ch"` // checksum
}

// ThingsExtension is the required xx field
type ThingsExtension struct {
	Sn  map[string]interface{} `json:"sn"`
	Typ string                 `json:"_t"` // always "oo"
}

// FullTaskPayload matches what Things actually sends
// Field order matches Proxyman capture exactly
type FullTaskPayload struct {
	Tp   int              `json:"tp"`            // type: 0=task, 1=project, 2=heading
	Sr   *int64           `json:"sr"`            // scheduled date (unix) - NULL for inbox/anytime
	Dds  *int64           `json:"dds"`           // deadline suppression date
	Rt   []string         `json:"rt"`            // repeating template IDs
	Rmd  *int64           `json:"rmd"`           // reminder date?
	Ss   int              `json:"ss"`            // status: 0=open, 3=complete
	Tr   bool             `json:"tr"`            // in trash
	Dl   []string         `json:"dl"`            // delegate
	Icp  bool             `json:"icp"`           // instance creation paused
	St   int              `json:"st"`            // schedule: 0=inbox, 1=anytime, 2=today
	Ar   []string         `json:"ar"`            // area IDs
	Tt   string           `json:"tt"`            // title
	Do   int              `json:"do"`            // due date offset
	Lai  *int64           `json:"lai"`           // last alarm interaction
	Tir  *int64           `json:"tir"`           // today index reference date - NULL for inbox/anytime
	Tg   []string         `json:"tg"`            // tag IDs
	Agr  []string         `json:"agr"`           // action group IDs
	Ix   int              `json:"ix"`            // index (Things uses negative values)
	Cd   float64          `json:"cd"`            // creation date
	Lt   bool             `json:"lt"`            // ?
	Icc  int              `json:"icc"`           // instance creation count
	Md   *float64         `json:"md"`            // modification date - NULL for new creates
	Ti   int              `json:"ti"`            // today index
	Dd   *int64           `json:"dd"`            // deadline (unix)
	Ato  *int             `json:"ato"`           // alarm time offset (seconds)
	Nt   ThingsNote       `json:"nt"`            // note
	Icsd *int64           `json:"icsd"`          // instance creation start date
	Pr   []string         `json:"pr"`            // parent/project IDs
	Rp   *string          `json:"rp"`            // ?
	Acrd *int64           `json:"acrd"`          // after completion reference date
	Sp   *float64         `json:"sp"`            // stop/completion date
	Sb   int              `json:"sb"`            // start bucket
	Rr   *json.RawMessage `json:"rr"`            // recurrence rule
	Xx   ThingsExtension  `json:"xx"`            // extension
}

// FullTaskItem wraps the payload for API
type FullTaskItem struct {
	T int             `json:"t"` // 0=create, 1=modify, 2=delete
	E string          `json:"e"` // "Task6"
	P FullTaskPayload `json:"p"`
}

func (f FullTaskItem) UUID() string {
	return "" // Will be set in the map key
}

// Wrapper to make it implement Identifiable
type TaskItemWrapper struct {
	uuid string
	item FullTaskItem
}

func (w TaskItemWrapper) UUID() string {
	return w.uuid
}

func (w TaskItemWrapper) MarshalJSON() ([]byte, error) {
	return json.Marshal(w.item)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: things-cli <command> [args]")
		fmt.Fprintln(os.Stderr, "Commands: list, today, inbox, area, project, show, create, edit, complete, delete, purge")
		os.Exit(1)
	}

	username := os.Getenv("THINGS_USERNAME")
	password := os.Getenv("THINGS_PASSWORD")
	if username == "" || password == "" {
		fmt.Fprintln(os.Stderr, "THINGS_USERNAME and THINGS_PASSWORD required")
		os.Exit(1)
	}

	c := thingscloud.New(thingscloud.APIEndpoint, username, password)
	c.Debug = true // Enable request/response logging
	if _, err := c.Verify(); err != nil {
		fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
		os.Exit(1)
	}

	history, err := c.OwnHistory()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get history: %v\n", err)
		os.Exit(1)
	}
	history.Sync()

	// Fetch all items
	var allItems []thingscloud.Item
	startIndex := 0
	for {
		items, hasMore, err := history.Items(thingscloud.ItemsOptions{StartIndex: startIndex})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to fetch items: %v\n", err)
			os.Exit(1)
		}
		allItems = append(allItems, items...)
		if !hasMore {
			break
		}
		startIndex = history.LoadedServerIndex
	}

	state := memory.NewState()
	state.Update(allItems...)

	cmd := os.Args[1]
	switch cmd {
	case "list":
		listTasks(state, false, false, "")
	case "today":
		listTasks(state, true, false, "")
	case "inbox":
		listTasks(state, false, true, "")
	case "area":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: things-cli area <name>")
			os.Exit(1)
		}
		listTasksByArea(state, os.Args[2])
	case "project":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: things-cli project <name>")
			os.Exit(1)
		}
		listTasksByProject(state, os.Args[2])
	case "show":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: things-cli show <uuid>")
			os.Exit(1)
		}
		showTask(state, os.Args[2])
	case "areas":
		listAreas(state)
	case "projects":
		listProjects(state)
	case "create":
		// things-cli create "Task title" [--note "note"] [--when today|someday|anytime|inbox] [--deadline 2026-02-15] [--scheduled 2026-02-12]
		createTaskFull(history, os.Args[2:])
	case "edit":
		// things-cli edit <uuid> [--title "new title"] [--note "new note"] [--when today|someday|anytime]
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: things-cli edit <uuid> [--title ...] [--note ...] [--when ...]")
			os.Exit(1)
		}
		editTask(history, os.Args[2], os.Args[3:])
	case "complete":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: things-cli complete <uuid>")
			os.Exit(1)
		}
		completeTask(history, os.Args[2])
	case "delete":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: things-cli delete <uuid>")
			os.Exit(1)
		}
		deleteTask(history, os.Args[2])
	case "fix-note":
		// Fix malformed note by pushing proper format
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: things-cli fix-note <uuid> [note text]")
			os.Exit(1)
		}
		noteText := ""
		if len(os.Args) > 3 {
			noteText = os.Args[3]
		}
		fixNote(history, os.Args[2], noteText)
	case "purge":
		// Push tombstone deletion
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: things-cli purge <uuid>")
			os.Exit(1)
		}
		purgeTask(history, os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

func listTasks(state *memory.State, todayOnly, inboxOnly bool, areaFilter string) {
	var tasks []TaskOutput
	for _, task := range state.Tasks {
		if task.InTrash || task.Status == 3 || task.Type == thingscloud.TaskTypeProject {
			continue
		}
		if todayOnly && task.Schedule != 2 {
			continue
		}
		if inboxOnly && task.Schedule != 0 {
			continue
		}
		tasks = append(tasks, taskToOutput(task))
	}
	outputJSON(tasks)
}

func listTasksByArea(state *memory.State, areaName string) {
	// Find area UUID
	var areaUUID string
	for _, area := range state.Areas {
		if strings.EqualFold(area.Title, areaName) {
			areaUUID = area.UUID
			break
		}
	}
	if areaUUID == "" {
		fmt.Fprintf(os.Stderr, "Area not found: %s\n", areaName)
		os.Exit(1)
	}

	var tasks []TaskOutput
	for _, task := range state.Tasks {
		if task.InTrash || task.Status == 3 {
			continue
		}
		for _, id := range task.AreaIDs {
			if id == areaUUID {
				tasks = append(tasks, taskToOutput(task))
				break
			}
		}
	}
	outputJSON(tasks)
}

func listTasksByProject(state *memory.State, projectName string) {
	// Find project UUID
	var projectUUID string
	for _, task := range state.Tasks {
		if task.Type == thingscloud.TaskTypeProject && strings.EqualFold(task.Title, projectName) {
			projectUUID = task.UUID
			break
		}
	}
	if projectUUID == "" {
		fmt.Fprintf(os.Stderr, "Project not found: %s\n", projectName)
		os.Exit(1)
	}

	var tasks []TaskOutput
	for _, task := range state.Tasks {
		if task.InTrash || task.Status == 3 || task.Type == thingscloud.TaskTypeProject {
			continue
		}
		for _, id := range task.ActionGroupIDs {
			if id == projectUUID {
				tasks = append(tasks, taskToOutput(task))
				break
			}
		}
	}
	outputJSON(tasks)
}

func showTask(state *memory.State, uuid string) {
	for _, task := range state.Tasks {
		if strings.HasPrefix(task.UUID, uuid) {
			outputJSON(taskToOutput(task))
			return
		}
	}
	fmt.Fprintf(os.Stderr, "Task not found: %s\n", uuid)
	os.Exit(1)
}

func listAreas(state *memory.State) {
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

func listProjects(state *memory.State) {
	var projects []TaskOutput
	for _, task := range state.Tasks {
		if task.Type == thingscloud.TaskTypeProject && !task.InTrash && task.Status != 3 {
			projects = append(projects, taskToOutput(task))
		}
	}
	outputJSON(projects)
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

func outputJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
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

func parseDate(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

func toUnixPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	ts := t.Unix()
	return &ts
}

// Generate Things-style short UUID (22 chars, base62-ish)
func generateThingsUUID() string {
	// Things uses a custom base62-like encoding
	// For simplicity, we'll use a UUID and take first 22 chars
	u := uuid.New()
	// Convert to base62-like string
	chars := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, 22)
	bytes := u[:]
	for i := 0; i < 22; i++ {
		result[i] = chars[int(bytes[i%16])%len(chars)]
	}
	return string(result)
}

func makeEmptyNote() ThingsNote {
	return ThingsNote{
		Typ: "tx",
		Ch:  0,
		V:   "",
		T:   1,
	}
}

func makeNoteWithText(text string) ThingsNote {
	// Things uses t:1 with text directly in v, checksum of the text
	var ch int64 = 0
	for _, c := range text {
		ch = (ch*31 + int64(c)) & 0xFFFFFFFF
	}
	return ThingsNote{
		Typ: "tx",
		Ch:  ch,
		V:   text,
		T:   1,
	}
}

func makeExtension() ThingsExtension {
	return ThingsExtension{
		Sn:  make(map[string]interface{}),
		Typ: "oo",
	}
}

func createTaskFull(history *thingscloud.History, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: things-cli create \"Title\" [options]")
		fmt.Fprintln(os.Stderr, "Options:")
		fmt.Fprintln(os.Stderr, "  --type task|project|heading    Type of item (default: task)")
		fmt.Fprintln(os.Stderr, "  --note \"...\"                   Note/description")
		fmt.Fprintln(os.Stderr, "  --when today|anytime|inbox     Schedule")
		fmt.Fprintln(os.Stderr, "  --deadline YYYY-MM-DD          Deadline date")
		fmt.Fprintln(os.Stderr, "  --scheduled YYYY-MM-DD         Scheduled date")
		fmt.Fprintln(os.Stderr, "  --project UUID                 Parent project UUID")
		fmt.Fprintln(os.Stderr, "  --heading UUID                 Parent heading UUID")
		fmt.Fprintln(os.Stderr, "  --area UUID                    Area UUID")
		fmt.Fprintln(os.Stderr, "  --tags UUID,UUID,...           Comma-separated tag UUIDs")
		fmt.Fprintln(os.Stderr, "  --uuid UUID                    Specify UUID (for overwrites)")
		os.Exit(1)
	}

	title := args[0]
	opts := parseArgs(args[1:])

	now := time.Now()
	nowTs := float64(now.UnixNano()) / 1e9
	
	// Allow specifying UUID for overwrite operations
	var taskUUID string
	if customUUID, ok := opts["uuid"]; ok && customUUID != "" {
		taskUUID = customUUID
	} else {
		taskUUID = generateThingsUUID()
	}

	// Determine item type (tp): 0=task, 1=project, 2=heading
	var tp int = 0
	if itemType, ok := opts["type"]; ok {
		switch itemType {
		case "project":
			tp = 1
		case "heading":
			tp = 2
		case "task":
			tp = 0
		}
	}

	// Determine schedule
	// Things uses null for sr/tir on inbox/anytime tasks, only sets for today/scheduled
	var st int = 0 // Inbox by default (matching Things behavior on new task)
	var sr *int64 = nil
	var tir *int64 = nil
	
	if when, ok := opts["when"]; ok {
		switch when {
		case "today":
			st = 2
			// For today, set sr/tir to UTC midnight
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			todayTs := today.Unix()
			sr = &todayTs
			tir = &todayTs
		case "someday", "anytime":
			st = 1
			// sr/tir stay null for anytime
		case "inbox":
			st = 0
			// sr/tir stay null for inbox
		}
	}

	// Build note
	var note ThingsNote
	if noteText, ok := opts["note"]; ok && noteText != "" {
		note = makeNoteWithText(noteText)
	} else {
		note = makeEmptyNote()
	}

	// Handle deadline
	var dd *int64 = nil
	if deadline, ok := opts["deadline"]; ok {
		if t := parseDate(deadline); t != nil {
			dd = toUnixPtr(t)
		}
	}

	// Handle scheduled date
	if scheduled, ok := opts["scheduled"]; ok {
		if t := parseDate(scheduled); t != nil {
			ts := t.Unix()
			sr = &ts
			tir = &ts
			if st < 2 {
				st = 2 // Move to Today/Scheduled if date set
			}
		}
	}

	// Handle parent project - MUST be empty array, not null
	pr := []string{}
	if projectUUID, ok := opts["project"]; ok && projectUUID != "" {
		pr = []string{projectUUID}
	}

	// Handle parent heading (after-group-reference) - MUST be empty array, not null
	agr := []string{}
	if headingUUID, ok := opts["heading"]; ok && headingUUID != "" {
		agr = []string{headingUUID}
	}

	// Handle area - MUST be empty array, not null
	ar := []string{}
	if areaUUID, ok := opts["area"]; ok && areaUUID != "" {
		ar = []string{areaUUID}
	}

	// Handle tags - MUST be empty array, not null
	tg := []string{}
	if tagsStr, ok := opts["tags"]; ok && tagsStr != "" {
		tg = strings.Split(tagsStr, ",")
	}

	// Build full payload matching Things format EXACTLY from Proxyman capture
	// Key: sr/tir null for inbox, md null for new creates, arrays always []
	payload := FullTaskPayload{
		Tp:   tp,
		Sr:   sr,    // null for inbox/anytime
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
		Tir:  tir,   // null for inbox/anytime
		Tg:   tg,
		Agr:  agr,
		Ix:   0,     // Things uses 0 or negative, 0 is safe
		Cd:   nowTs, // fractional unix timestamp
		Lt:   false,
		Icc:  0,
		Md:   nil,   // NULL for new creates! Things sets this on first edit
		Ti:   0,
		Dd:   dd,
		Ato:  nil,
		Nt:   note,
		Icsd: nil,
		Pr:   pr,
		Rp:   nil,
		Acrd: nil,
		Sp:   nil,
		Sb:   0,
		Rr:   nil,
		Xx:   makeExtension(),
	}

	item := FullTaskItem{
		T: 0, // Create
		E: "Task6",
		P: payload,
	}

	// Debug: print what we're about to send
	debugPayload := map[string]interface{}{taskUUID: item}
	debugBytes, _ := json.MarshalIndent(debugPayload, "", "  ")
	fmt.Fprintf(os.Stderr, "DEBUG payload:\n%s\n", string(debugBytes))

	// Write using proper envelope format
	writeItem := directWriteItem{uuid: taskUUID, item: item}
	if err := history.Write(writeItem); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create task: %v\n", err)
		os.Exit(1)
	}

	outputJSON(map[string]string{
		"status": "created",
		"uuid":   taskUUID,
		"title":  title,
	})
}

// directWriteItem wraps a FullTaskItem for the SDK's Write method
// The SDK calls MarshalJSON on each item and puts it in map[uuid]item
// So we need MarshalJSON to return the {t, e, p} structure
type directWriteItem struct {
	uuid string
	item FullTaskItem
}

func (d directWriteItem) UUID() string {
	return d.uuid
}

func (d directWriteItem) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.item)
}

// directWriteUpdate wraps an update payload (only changed fields)
type directWriteUpdate struct {
	uuid    string
	payload map[string]interface{}
}

func (d directWriteUpdate) UUID() string {
	return d.uuid
}

func (d directWriteUpdate) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.payload)
}

func editTask(history *thingscloud.History, taskUUID string, args []string) {
	opts := parseArgs(args)

	if len(opts) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: things-cli edit <uuid> [--title \"...\"] [--note \"...\"] [--when today|someday|anytime] [--deadline YYYY-MM-DD] [--scheduled YYYY-MM-DD]")
		os.Exit(1)
	}

	now := time.Now()
	nowTs := float64(now.UnixNano()) / 1e9

	// Build payload with only changed fields (Things update format)
	p := map[string]interface{}{
		"md": nowTs, // modification date always required
	}

	if title, ok := opts["title"]; ok {
		p["tt"] = title
	}

	if note, ok := opts["note"]; ok {
		if note == "" {
			p["nt"] = map[string]interface{}{"t": 1, "ch": 0, "v": "", "_t": "tx"}
		} else {
			// Simple note with checksum
			var ch int64 = 0
			for _, c := range note {
				ch = (ch*31 + int64(c)) & 0xFFFFFFFF
			}
			p["nt"] = map[string]interface{}{"t": 1, "ch": ch, "v": note, "_t": "tx"}
		}
	}

	if when, ok := opts["when"]; ok {
		switch when {
		case "today":
			p["st"] = 2
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			todayTs := today.Unix()
			p["sr"] = todayTs
			p["tir"] = todayTs
		case "someday", "anytime":
			p["st"] = 1
			p["sr"] = nil
			p["tir"] = nil
		case "inbox":
			p["st"] = 0
			p["sr"] = nil
			p["tir"] = nil
		}
	}

	if deadline, ok := opts["deadline"]; ok {
		if t := parseDate(deadline); t != nil {
			p["dd"] = t.Unix()
		}
	}

	if scheduled, ok := opts["scheduled"]; ok {
		if t := parseDate(scheduled); t != nil {
			p["sr"] = t.Unix()
			p["tir"] = t.Unix()
			if _, hasWhen := opts["when"]; !hasWhen {
				p["st"] = 2 // Move to scheduled if not explicitly set
			}
		}
	}

	// Build update envelope: {t: 1, e: "Task6", p: {...}}
	envelope := map[string]interface{}{
		"t": 1,
		"e": "Task6",
		"p": p,
	}

	writeItem := directWriteUpdate{uuid: taskUUID, payload: envelope}
	if err := history.Write(writeItem); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to edit task: %v\n", err)
		os.Exit(1)
	}

	outputJSON(map[string]string{
		"status": "updated",
		"uuid":   taskUUID,
	})
}

func completeTask(history *thingscloud.History, taskUUID string) {
	now := time.Now()
	nowTs := float64(now.UnixNano()) / 1e9

	// Complete: ss=3 (completed), sp=completion timestamp
	p := map[string]interface{}{
		"md": nowTs,
		"ss": 3,     // status = completed
		"sp": nowTs, // stop/completion date
	}

	envelope := map[string]interface{}{
		"t": 1,
		"e": "Task6",
		"p": p,
	}

	writeItem := directWriteUpdate{uuid: taskUUID, payload: envelope}
	if err := history.Write(writeItem); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to complete task: %v\n", err)
		os.Exit(1)
	}

	outputJSON(map[string]string{
		"status": "completed",
		"uuid":   taskUUID,
	})
}

func deleteTask(history *thingscloud.History, taskUUID string) {
	now := time.Now()
	nowTs := float64(now.UnixNano()) / 1e9

	// Move to trash: tr=true
	p := map[string]interface{}{
		"md": nowTs,
		"tr": true,
	}

	envelope := map[string]interface{}{
		"t": 1,
		"e": "Task6",
		"p": p,
	}

	writeItem := directWriteUpdate{uuid: taskUUID, payload: envelope}
	if err := history.Write(writeItem); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to delete task: %v\n", err)
		os.Exit(1)
	}

	outputJSON(map[string]string{
		"status": "trashed",
		"uuid":   taskUUID,
	})
}

func purgeTask(history *thingscloud.History, taskUUID string) {
	now := time.Now()
	nowTs := float64(now.UnixNano()) / 1e9

	// Create a Tombstone2 entry to permanently delete
	// Things uses: {"t":0,"e":"Tombstone2","p":{"dloid":"UUID","dld":timestamp}}
	tombstoneUUID := generateThingsUUID()
	
	envelope := map[string]interface{}{
		"t": 0, // Create (we're creating a tombstone)
		"e": "Tombstone2",
		"p": map[string]interface{}{
			"dloid": taskUUID, // deleted object ID
			"dld":   nowTs,    // deletion date
		},
	}

	writeItem := directWriteUpdate{uuid: tombstoneUUID, payload: envelope}
	if err := history.Write(writeItem); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to purge task: %v\n", err)
		os.Exit(1)
	}

	outputJSON(map[string]string{
		"status": "purged",
		"uuid":   taskUUID,
	})
}

func fixNote(history *thingscloud.History, taskUUID string, noteText string) {
	now := time.Now()
	nowTs := float64(now.UnixNano()) / 1e9

	// Build note object
	var noteObj map[string]interface{}
	if noteText == "" {
		noteObj = map[string]interface{}{"t": 1, "ch": 0, "v": "", "_t": "tx"}
	} else {
		var ch int64 = 0
		for _, c := range noteText {
			ch = (ch*31 + int64(c)) & 0xFFFFFFFF
		}
		noteObj = map[string]interface{}{"t": 1, "ch": ch, "v": noteText, "_t": "tx"}
	}

	p := map[string]interface{}{
		"md": nowTs,
		"nt": noteObj,
	}

	envelope := map[string]interface{}{
		"t": 1,
		"e": "Task6",
		"p": p,
	}

	writeItem := directWriteUpdate{uuid: taskUUID, payload: envelope}
	if err := history.Write(writeItem); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fix note: %v\n", err)
		os.Exit(1)
	}

	outputJSON(map[string]string{
		"status": "fixed",
		"uuid":   taskUUID,
	})
}
