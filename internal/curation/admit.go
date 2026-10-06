package curation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/lifecycle"
)

// Verdict is the file an outside curator writes after judging a packet.
type Verdict struct {
	Word    string `json:"word"`
	Packet  string `json:"packet"`
	Verdict string `json:"verdict"`
	Curator string `json:"curator"`
	Reason  string `json:"reason"`
}

var packetID = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Admit records a curator's verdict on a proposed word. Both verdicts are kept beside the
// packet; only "admit" writes an admission record, so a rejected word stays as it was. The
// verdict must name a packet of this word whose files and sections still match the
// project, so a curator never admits something other than what it judged.
func Admit(p dictionary.Project, name, verdictPath string) (Verdict, *lifecycle.Admission, error) {
	var v Verdict
	data, err := os.ReadFile(verdictPath)
	if err != nil {
		return v, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&v); err != nil {
		return v, nil, fmt.Errorf("%s: not a curator verdict: %v", verdictPath, err)
	}
	switch {
	case v.Word != name:
		return v, nil, fmt.Errorf("%s: the verdict is for %q, not %s", verdictPath, v.Word, name)
	case v.Verdict != "admit" && v.Verdict != "reject":
		return v, nil, fmt.Errorf("%s: 'verdict' must be \"admit\" or \"reject\"", verdictPath)
	case strings.TrimSpace(v.Curator) == "":
		return v, nil, fmt.Errorf("%s: 'curator' must name the model that judged", verdictPath)
	case !packetID.MatchString(v.Packet):
		return v, nil, fmt.Errorf("%s: 'packet' must be the id `verilex propose %s` printed", verdictPath, name)
	}
	packetPath := filepath.Join(proposalsDir(p, name), v.Packet+".json")
	var packet Packet
	data, err = os.ReadFile(packetPath)
	if errors.Is(err, fs.ErrNotExist) {
		return v, nil, fmt.Errorf("no packet %s was proposed for %s", v.Packet, name)
	}
	if err != nil {
		return v, nil, err
	}
	if err = json.Unmarshal(data, &packet); err != nil || packet.Word != name || packet.ID != v.Packet {
		return v, nil, fmt.Errorf("%s: not a packet for %s", packetPath, name)
	}
	words, err := lifecycle.LoadWords(p)
	if err != nil {
		return v, nil, err
	}
	w, err := find(words, name)
	if err != nil {
		return v, nil, err
	}
	stale := fmt.Errorf("%s changed since packet %s; propose it again", name, v.Packet)
	digest, err := lifecycle.WordDigest(w)
	if err != nil {
		return v, nil, err
	}
	current, err := sections(p, w)
	if err != nil {
		return v, nil, fmt.Errorf("%v; %v", err, stale)
	}
	if digest != packet.WordDigest || len(current) != len(packet.Sections) || w.Stale != "" {
		return v, nil, stale
	}
	proves, err := claimPacket(p, w)
	if err != nil {
		return v, nil, fmt.Errorf("%v; %v", err, stale)
	}
	if !sameClaim(proves, packet.Claim) {
		return v, nil, stale
	}
	hashes := map[string]string{}
	for i, section := range current {
		if section.Ref != packet.Sections[i].Ref || section.Hash != packet.Sections[i].Hash {
			return v, nil, stale
		}
		hashes[section.Ref] = section.Hash
	}
	if err = writeJSON(filepath.Join(proposalsDir(p, name), v.Packet+".verdict.json"), v); err != nil {
		return v, nil, err
	}
	if v.Verdict == "reject" {
		return v, nil, nil
	}
	runs := make([]string, 0, len(packet.Uses))
	for _, use := range packet.Uses {
		runs = append(runs, use.Run)
	}
	a := &lifecycle.Admission{Word: name, Date: now(), Curator: v.Curator, Reason: v.Reason, Packet: v.Packet, Runs: runs, WordDigest: digest, Sections: hashes}
	if proves != nil {
		a.Claim, a.ClaimSources, a.Sections = proves.Pin, w.Claim.SourcesDigest(), nil
	}
	return v, a, writeJSON(lifecycle.AdmissionPath(w), a)
}

// sameClaim reports whether a word proves the claim version its packet showed, anchored in the
// same requirement sentences.
func sameClaim(current, packed *ClaimPacket) bool {
	if current == nil || packed == nil {
		return current == packed
	}
	if current.Pin != packed.Pin || current.Entry != packed.Entry || len(current.Sources) != len(packed.Sources) {
		return false
	}
	for i, source := range current.Sources {
		if source.Ref != packed.Sources[i].Ref || source.Hash != packed.Sources[i].Hash {
			return false
		}
	}
	return true
}
