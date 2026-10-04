package tests

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DereKk8/verilex/internal/curation"
	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
	"github.com/DereKk8/verilex/internal/lifecycle"
)

const itemAdd = "verify-tally/features/items.md#item-add"

func feature(root, file string) string {
	return filepath.Join(root, ".cursor", "skills", "verify-tally", "features", file)
}

func used(t *testing.T, root, chain string) {
	t.Helper()
	done := verilex(t, root, nil, "run", chain)
	if done.code != 0 {
		t.Fatalf("run %q: %d\n%s%s", chain, done.code, done.stdout, done.stderr)
	}
}

func propose(t *testing.T, root, word string) curation.Packet {
	t.Helper()
	done := verilex(t, root, nil, "propose", word)
	if done.code != 0 {
		t.Fatalf("propose %s: %d %s", word, done.code, done.stderr)
	}
	path := regexp.MustCompile(`packet: (\S+)`).FindStringSubmatch(done.stdout)[1]
	var packet curation.Packet
	if err := json.Unmarshal([]byte(read(t, path)), &packet); err != nil {
		t.Fatal(err)
	}
	return packet
}

func verdict(t *testing.T, root, body string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(root), "verdict.json")
	write(t, path, body, 0644)
	return path
}

func admit(t *testing.T, root, word string) curation.Packet {
	t.Helper()
	packet := propose(t, root, word)
	file := verdict(t, root, `{"word": "`+word+`", "packet": "`+packet.ID+`", "verdict": "admit", "curator": "curator-model-1", "reason": "One real moment, proven twice."}`)
	done := verilex(t, root, nil, "admit", word, "--verdict", file)
	if done.code != 0 {
		t.Fatalf("admit %s: %d %s", word, done.code, done.stderr)
	}
	return packet
}

func status(t *testing.T, root, word string) string {
	t.Helper()
	listing := verilex(t, root, nil, "words").stdout
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(word) + `\b[^\n]*\n(?:  [^\n]*\n)*?  status:   (\S+)`).FindStringSubmatch(listing)
	if m == nil {
		t.Fatalf("%s not listed:\n%s", word, listing)
	}
	return m[1]
}

func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files[path] = read(t, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestNewScaffoldsProvisionalWordThatNeverPasses(t *testing.T) {
	root := product(t)
	done := verilex(t, root, nil, "new", "item-counted", "--implements", itemAdd)
	equal(t, done.code, 0)
	contains(t, done.stdout, "new: provisional word item-counted implements "+itemAdd)
	dir := filepath.Join(root, ".verilex", "words", "item-counted")
	contains(t, read(t, filepath.Join(dir, "word.md")), "implements:\n  - \""+itemAdd+"\"\n")
	equal(t, status(t, root, "item-counted"), "provisional")

	done = verilex(t, root, nil, "run", "item-counted")
	equal(t, done.code, 2)
	equal(t, verdicts(lastRun(t, root)), []string{"inconclusive"})

	done = verilex(t, root, nil, "new", "item-sold", "--implements", "verify-tally/features/items.md#item-sell")
	equal(t, done.code, 2)
	contains(t, done.stderr, "has no section \"item-sell\"")
	contains(t, done.stderr, "verilex gap")
	done = verilex(t, root, nil, "new", "item-sold", "--implements", "verify-tally/../../bin/tally")
	equal(t, done.code, 2)
	contains(t, done.stderr, "not a <skill>/<file>#<section> reference")
	done = verilex(t, root, nil, "new", "item-sold")
	equal(t, done.code, 2)
	contains(t, done.stderr, "must implement a verify-skill feature-map section")
	if _, err := os.Stat(filepath.Join(root, ".verilex", "words", "item-sold")); !os.IsNotExist(err) {
		t.Fatalf("a refused word was scaffolded: %v", err)
	}
	done = verilex(t, root, nil, "new", "store-open", "--implements", "verify-tally/features/store.md#store-open")
	equal(t, done.code, 2)
	contains(t, done.stderr, "already exists")
}

func TestNewCreatesVerilexWithFrameStubsInBareProject(t *testing.T) {
	root := product(t)
	if err := os.RemoveAll(filepath.Join(root, ".verilex")); err != nil {
		t.Fatal(err)
	}
	done := verilex(t, root, nil, "new", "store-open", "--implements", "verify-tally/features/store.md#store-open")
	equal(t, done.code, 0)
	contains(t, done.stdout, "frame stubs (launch, doctor, refresh, cleanup)")
	equal(t, read(t, filepath.Join(root, ".verilex", "config.yaml")), "project: tally\n")
	for _, step := range []string{"launch", "doctor", "refresh", "cleanup"} {
		info, err := os.Stat(filepath.Join(root, ".verilex", "frame", step))
		if err != nil || info.Mode().Perm()&0100 == 0 {
			t.Fatalf("frame/%s is not an executable stub: %v", step, err)
		}
	}
	equal(t, status(t, root, "store-open"), "provisional")
	done = verilex(t, root, nil, "run", "store-open")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, string(*record.Verdict), "inconclusive")
	contains(t, *record.Reason, "launch exited 2")
}

func TestProposeNeedsUsesInTwoDifferentRuns(t *testing.T) {
	root := product(t)
	done := verilex(t, root, nil, "propose", "item-stored")
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: item-stored has counted uses in 0 run(s); propose needs at least 2 different runs in which it was green or red\n")

	used(t, root, "store-open | item-stored apple | item-stored pear")
	done = verilex(t, root, nil, "propose", "item-stored")
	equal(t, done.code, 2)
	contains(t, done.stderr, "counted uses in 1 run(s)")

	used(t, root, "store-open | item-stored fig")
	packet := propose(t, root, "item-stored")
	equal(t, packet.Word, "item-stored")
	equal(t, packet.Status, lifecycle.Provisional)
	equal(t, len(packet.Uses), 2)
	if packet.Uses[0].Run == packet.Uses[1].Run {
		t.Fatalf("uses share a run: %#v", packet.Uses)
	}
	for _, use := range packet.Uses {
		equal(t, string(use.Verdict), "green")
		if info, err := os.Stat(use.Evidence); err != nil || !info.IsDir() {
			t.Fatalf("use evidence %s: %v", use.Evidence, err)
		}
	}
	equal(t, len(packet.Sections), 1)
	equal(t, packet.Sections[0].Ref, itemAdd)
	contains(t, packet.Sections[0].Text, "`item-add` stores a named item.")
	contains(t, packet.Files["word.md"], "word: item-stored")
	contains(t, packet.Files["run"], "tally(\"add\", name)")
	names := []string{}
	for _, entry := range packet.Dictionary {
		names = append(names, entry.Word+"="+string(entry.Status))
	}
	equal(t, names, []string{"item-listed=provisional", "item-stored=provisional", "store-open=provisional"})
	contains(t, packet.Curator, "verilex admit <word> --verdict <file>")
}

func TestAdmitRecordsCuratorVerdict(t *testing.T) {
	root := product(t)
	used(t, root, "store-open | item-stored apple")
	used(t, root, "store-open | item-stored pear")
	before := time.Now().UTC().Add(-time.Second)
	packet := propose(t, root, "item-stored")
	file := verdict(t, root, `{"word": "item-stored", "packet": "`+packet.ID+`", "verdict": "admit", "curator": "curator-model-1", "reason": "Proven twice."}`)
	done := verilex(t, root, nil, "admit", "item-stored", "--verdict", file)
	equal(t, done.code, 0)
	equal(t, done.stdout, "admitted item-stored (curator curator-model-1, 2 runs)\n")

	var admission lifecycle.Admission
	if err := json.Unmarshal([]byte(read(t, filepath.Join(root, ".verilex", "words", "item-stored", "admission.json"))), &admission); err != nil {
		t.Fatal(err)
	}
	date, err := time.Parse(time.RFC3339, admission.Date)
	if err != nil || date.Before(before) {
		t.Fatalf("admission date %q: %v", admission.Date, err)
	}
	equal(t, admission.Curator, "curator-model-1")
	equal(t, admission.Packet, packet.ID)
	equal(t, admission.Runs, []string{packet.Uses[0].Run, packet.Uses[1].Run})
	equal(t, admission.Sections, map[string]string{itemAdd: packet.Sections[0].Hash})
	equal(t, status(t, root, "item-stored"), "admitted")
	equal(t, status(t, root, "store-open"), "provisional")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (1 admitted)\n")

	done = verilex(t, root, nil, "propose", "item-stored")
	equal(t, done.code, 2)
	contains(t, done.stderr, "already admitted")
}

func TestRejectVerdictLeavesWordProvisional(t *testing.T) {
	root := product(t)
	used(t, root, "store-open | item-stored apple")
	used(t, root, "store-open | item-stored pear")
	packet := propose(t, root, "item-stored")
	file := verdict(t, root, `{"word": "item-stored", "packet": "`+packet.ID+`", "verdict": "reject", "curator": "curator-model-1", "reason": "Overlaps another word."}`)
	done := verilex(t, root, nil, "admit", "item-stored", "--verdict", file)
	equal(t, done.code, 0)
	equal(t, done.stdout, "rejected item-stored (curator curator-model-1): stays provisional\n")
	if _, err := os.Stat(filepath.Join(root, ".verilex", "words", "item-stored", "admission.json")); !os.IsNotExist(err) {
		t.Fatalf("a rejected word has an admission record: %v", err)
	}
	equal(t, status(t, root, "item-stored"), "provisional")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (0 admitted)\n")
}

func TestAdmitRefusesVerdictThatDoesNotMatchItsPacket(t *testing.T) {
	root := product(t)
	used(t, root, "store-open | item-stored apple")
	used(t, root, "store-open | item-stored pear")
	packet := propose(t, root, "item-stored")
	for body, refusal := range map[string]string{
		`{"word": "item-stored", "packet": "0123456789abcdef", "verdict": "admit", "curator": "m"}`:  "no packet 0123456789abcdef was proposed",
		`{"word": "store-open", "packet": "` + packet.ID + `", "verdict": "admit", "curator": "m"}`:  "the verdict is for \"store-open\"",
		`{"word": "item-stored", "packet": "` + packet.ID + `", "verdict": "admit"}`:                 "'curator' must name the model",
		`{"word": "item-stored", "packet": "` + packet.ID + `", "verdict": "maybe", "curator": "m"}`: "'verdict' must be",
		`{"word": "item-stored", "packet": "` + packet.ID + `", "verdict": "admit", "curatr": "m"}`:  "unknown field",
		`{"word": "item-stored", "packet": "../../../x", "verdict": "admit", "curator": "m"}`:        "'packet' must be the id",
	} {
		done := verilex(t, root, nil, "admit", "item-stored", "--verdict", verdict(t, root, body))
		equal(t, done.code, 2)
		contains(t, done.stderr, refusal)
	}
	if _, err := os.Stat(filepath.Join(root, ".verilex", "words", "item-stored", "admission.json")); !os.IsNotExist(err) {
		t.Fatalf("a refused verdict admitted the word: %v", err)
	}

	items := feature(root, "items.md")
	write(t, items, read(t, items)+"- Adding an item twice keeps one entry.\n", 0644)
	file := verdict(t, root, `{"word": "item-stored", "packet": "`+packet.ID+`", "verdict": "admit", "curator": "m"}`)
	done := verilex(t, root, nil, "admit", "item-stored", "--verdict", file)
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: item-stored changed since packet "+packet.ID+"; propose it again\n")
	equal(t, status(t, root, "item-stored"), "provisional")
}

func TestEditingFeatureMapSectionMarksWordDriftSuspect(t *testing.T) {
	root := product(t)
	used(t, root, "store-open | item-stored apple")
	used(t, root, "store-open | item-stored pear")
	admit(t, root, "item-stored")
	admit(t, root, "store-open")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (2 admitted)\n")

	items := feature(root, "items.md")
	original := read(t, items)
	write(t, items, strings.Replace(original, "Expect exit 0 and `added NAME`", "Expect exit 0 and `stored NAME`", 1), 0644)
	done := verilex(t, root, nil, "check")
	equal(t, done.code, 0)
	equal(t, done.stdout, "check: 1 of 2 admitted drift-suspect; they always run\n  item-stored: "+itemAdd+": section changed\n")
	equal(t, status(t, root, "item-stored"), "drift-suspect")
	equal(t, status(t, root, "store-open"), "admitted")

	write(t, items, strings.Replace(original, "`item-add`", "`item-put`", 1), 0644)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 2 admitted drift-suspect; they always run\n  item-stored: "+itemAdd+": section missing\n")

	write(t, items, original, 0644)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (2 admitted)\n")

	run := filepath.Join(root, ".verilex", "words", "store-open", "run")
	write(t, run, read(t, run)+"\n", 0755)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 2 admitted drift-suspect; they always run\n  store-open: the word's files changed since admission\n")

	packet := propose(t, root, "store-open")
	equal(t, packet.Status, lifecycle.DriftSuspect)
}

func TestGapRecordsNoteWithoutTouchingVerifySkill(t *testing.T) {
	root := product(t)
	skill := filepath.Join(root, ".cursor")
	before := tree(t, skill)
	done := verilex(t, root, nil, "gap", "A user renames an item")
	equal(t, done.code, 0)
	path := strings.TrimPrefix(strings.TrimSpace(done.stdout), "gap recorded for the verify skill's owner: ")
	equal(t, filepath.Dir(path), filepath.Join(root, ".verilex", "gaps"))
	contains(t, filepath.Base(path), "-a-user-renames-an-item.md")
	contains(t, read(t, path), "# Gap: A user renames an item\n")
	second := verilex(t, root, nil, "gap", "A user renames an item")
	equal(t, second.code, 0)
	if second.stdout == done.stdout {
		t.Fatal("a second gap overwrote the first")
	}
	equal(t, tree(t, skill), before)
	done = verilex(t, root, nil, "gap", "  ")
	equal(t, done.code, 2)
	contains(t, done.stderr, "describe the product moment")
}

// curated copies tally with every word admitted, standing in for the propose and admit flow
// tested above, so tests about skipping start from words that are allowed to skip.
func curated(t *testing.T) string {
	t.Helper()
	root := product(t)
	admitted(t, root)
	return root
}

// admitted writes an admission record for each named word (every word when none is named)
// that matches the word's files and feature-map sections as they are now.
func admitted(t *testing.T, root string, names ...string) {
	t.Helper()
	project, err := dictionary.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	words, err := dictionary.LoadWords(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range words {
		if len(names) > 0 && !slices.Contains(names, word.Name) {
			continue
		}
		digest, err := lifecycle.WordDigest(word)
		if err != nil {
			t.Fatal(err)
		}
		admission := lifecycle.Admission{Word: word.Name, Date: time.Now().UTC().Format(time.RFC3339), Curator: "test-curator", Packet: "0123456789abcdef", Runs: []string{"1-a", "2-b"}, WordDigest: digest, Sections: map[string]string{}}
		for _, ref := range word.Implements {
			section, err := featuremap.Resolve(project.Root, project.SkillDirs, ref)
			if err != nil {
				t.Fatal(err)
			}
			admission.Sections[ref] = section.Hash
		}
		data, err := json.Marshal(admission)
		if err != nil {
			t.Fatal(err)
		}
		write(t, lifecycle.AdmissionPath(word), string(data), 0644)
		if s := status(t, root, word.Name); s != "admitted" {
			t.Fatalf("%s is %s after admission", word.Name, s)
		}
	}
}

// Rule: only admitted words are skipped. A provisional word's results are recorded, but a chain
// that holds one always runs live, even when every stamp matches.
func TestChainWithProvisionalWordIsNeverSkipped(t *testing.T) {
	root := product(t)
	green(t, root, nil, chain)
	record := green(t, root, nil, chain)
	equal(t, record.Rerun, "store-open: provisional; only admitted words are skipped")
	ranLive(t, record)

	admitted(t, root, "store-open", "item-stored")
	green(t, root, nil, chain)
	for i := 0; i < 2; i++ {
		record = green(t, root, nil, chain)
		equal(t, record.Rerun, "item-listed apple: provisional; only admitted words are skipped")
		ranLive(t, record)
	}
	// The admitted prefix is not held, so it is skipped on its own.
	prefix := green(t, root, nil, "store-open | item-stored apple")
	equal(t, prefix.Skipped, true)

	admitted(t, root, "item-listed")
	proof := green(t, root, nil, chain)
	equal(t, proof.Rerun, "item-listed apple: word changed")
	ranLive(t, proof)
	record = green(t, root, nil, chain)
	equal(t, record.Skipped, true)
	equal(t, record.Words[2].ReliesOn, proof.Run)
}

// Rule: a drift-suspect word always runs, although nothing its stamp covers changed.
func TestChainWithDriftSuspectWordIsNeverSkipped(t *testing.T) {
	root := curated(t)
	first := green(t, root, nil, chain)
	equal(t, green(t, root, nil, chain).Skipped, true)

	items := feature(root, "items.md")
	original := read(t, items)
	write(t, items, strings.Replace(original, "Expect exit 0 and `added NAME`", "Expect exit 0 and `stored NAME`", 1), 0644)
	equal(t, status(t, root, "item-stored"), "drift-suspect")
	for i := 0; i < 2; i++ {
		record := green(t, root, nil, chain)
		equal(t, record.Rerun, "item-stored apple: drift-suspect: "+itemAdd+": section changed")
		ranLive(t, record)
		equal(t, record.Words[1].Stamp, first.Words[1].Stamp)
	}
	write(t, items, strings.Replace(original, "`item-add`", "`item-put`", 1), 0644)
	record := green(t, root, nil, chain)
	equal(t, record.Rerun, "item-stored apple: drift-suspect: "+itemAdd+": section missing")
	ranLive(t, record)

	// An admission record that cannot be read holds the word too.
	write(t, items, original, 0644)
	equal(t, green(t, root, nil, chain).Skipped, true)
	admission := filepath.Join(root, ".verilex", "words", "item-listed", "admission.json")
	saved := read(t, admission)
	write(t, admission, "{not json", 0644)
	green(t, root, nil, chain)
	record = green(t, root, nil, chain)
	contains(t, record.Rerun, "item-listed apple: lifecycle status unreadable: ")
	ranLive(t, record)

	// Restoring the record lifts the hold. The record is part of the word's stamp, and the
	// ledger now holds results proven under the unreadable one, so the chain runs once more.
	write(t, admission, saved, 0644)
	record = green(t, root, nil, chain)
	equal(t, record.Rerun, "item-listed apple: word changed")
	ranLive(t, record)
	equal(t, green(t, root, nil, chain).Skipped, true)
}

// Rule: a use is a green or red step in a live run that was itself green or red. Inconclusive
// steps, inconclusive runs and skipped runs never drove the product to a verdict.
func TestOnlyJudgedLiveRunsCountAsUses(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	equal(t, green(t, root, nil, chain).Skipped, true)
	runJSON(t, root, map[string]string{"TALLY_SIMULATE_LOCK": "1"}, chain)
	cleanup := filepath.Join(root, ".verilex", "frame", "cleanup")
	appendTo(t, cleanup, "\nif os.environ.get(\"LEAK\"):\n    (Path(os.environ[\"VERILEX_EVIDENCE\"]) / \"key.pem\").write_text(\"-----BEGIN PRIVATE KEY-----\\n\")\n")
	done, leaked := runJSON(t, root, map[string]string{"LEAK": "1"}, chain)
	equal(t, done.code, 2)
	equal(t, verdicts(leaked), []string{"green", "green", "green"})

	items := feature(root, "items.md")
	write(t, items, read(t, items)+"- Adding an item twice keeps one entry.\n", 0644)
	equal(t, status(t, root, "item-stored"), "drift-suspect")
	done = verilex(t, root, nil, "propose", "item-stored")
	equal(t, done.code, 2)
	contains(t, done.stderr, "item-stored has counted uses in 1 run(s)")

	done = verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, "run", chain)
	equal(t, done.code, 1)
	packet := propose(t, root, "item-stored")
	equal(t, len(packet.Uses), 2)
	equal(t, string(packet.Uses[0].Verdict), "green")
	equal(t, string(packet.Uses[1].Verdict), "red")
}
