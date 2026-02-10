package thingscloud

// NoteTypeFullText indicates a note with complete text
const NoteTypeFullText = 1

// NoteTypeDelta indicates a note with incremental patches
const NoteTypeDelta = 2

// NotePatch describes a single text replacement operation
type NotePatch struct {
	Replacement string `json:"r"`
	Position    int    `json:"p"`
	Length      int    `json:"l"`
	Checksum    int64  `json:"ch"`
}

// Note describes a structured note as used by the Things API
type Note struct {
	TypeTag  string      `json:"_t"`
	Type     int         `json:"t"`
	Checksum int64       `json:"ch,omitempty"`
	Value    string      `json:"v,omitempty"`
	Patches  []NotePatch `json:"ps,omitempty"`
}

// ApplyPatches applies a series of text patches to an original string
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
