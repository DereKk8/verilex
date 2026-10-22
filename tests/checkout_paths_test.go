package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DereKk8/verilex/internal/curation"
	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/fingerprint"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/lifecycle"
	"github.com/DereKk8/verilex/internal/runner"
)

// admittedSkippingCheckout is a tally checkout whose three words are admitted and whose
// default chain already has a green run, so check is clean and plan skips.
func admittedSkippingCheckout(t *testing.T) string {
	t.Helper()
	root := baseline(t)
	used(t, root, chain)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
	plan := verilex(t, root, nil, "plan", chain)
	if !strings.HasPrefix(plan.stdout, "plan: skip 3, run 0\n") {
		t.Fatalf("baseline plan: %q", plan.stdout)
	}
	return root
}

// verilexIn runs the CLI with working directory dir. root is still the product: pass
// --project root when dir is not the product.
func verilexIn(t *testing.T, root, dir string, args ...string) output {
	t.Helper()
	cmd, stdout, stderr := command(root, nil, args...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return (&process{cmd, stdout, stderr}).wait(t)
}

func groupWritable(t *testing.T, root string) {
	t.Helper()
	chmodAll(t, root, func(perm fs.FileMode) fs.FileMode { return perm | 0o020 })
}

// chmodAll gives root and everything under it the permission bits change makes of its own.
func chmodAll(t *testing.T, root string, change func(fs.FileMode) fs.FileMode) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.Chmod(path, change(info.Mode().Perm()))
	})
	if err != nil {
		t.Fatal(err)
	}
}

func chmod(t *testing.T, path string, mode fs.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func stampsOf(t *testing.T, raw string) []string {
	t.Helper()
	var rec runner.Record
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatal(err)
	}
	if len(rec.Words) != 3 {
		t.Fatalf("words: %+v", rec.Words)
	}
	stamps := make([]string, len(rec.Words))
	for i, word := range rec.Words {
		if word.Stamp == "" {
			t.Fatalf("%s has no stamp in %s", word.Word, raw)
		}
		stamps[i] = word.Word + "=" + word.Stamp
	}
	return stamps
}

// Group-write bits are not part of the commit. A checkout that differs only by those
// bits keeps the same proof-stamp digests, and check reports no drift.
func TestGroupWritableCheckoutKeepsDigestsAndSkips(t *testing.T) {
	root := admittedSkippingCheckout(t)
	beforeRun := verilex(t, root, nil, "run", "--json", chain)
	equal(t, beforeRun.code, 0)
	beforeStamps := stampsOf(t, beforeRun.stdout)
	beforePlan := verilex(t, root, nil, "plan", chain).stdout
	if !strings.HasPrefix(beforePlan, "plan: skip 3, run 0\n") {
		t.Fatalf("baseline plan: %q", beforePlan)
	}

	groupWritable(t, filepath.Join(root, ".verilex"))

	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
	equal(t, verilex(t, root, nil, "plan", chain).stdout, beforePlan)
	afterRun := verilex(t, root, nil, "run", "--json", chain)
	equal(t, afterRun.code, 0)
	equal(t, stampsOf(t, afterRun.stdout), beforeStamps)
}

const changedTally = "changed: 3 claim(s); 0 of 3 known chain(s) must re-run\n" +
	"item-added  A named item is in the store.\n" +
	"item-listed  A stored item shows up when a user lists the store.\n" +
	"store-opened  A new, empty store is open and ready for items.\n" +
	"  3 chain(s) still skip: `verilex plan '<chain>'` cites the run each relies on\n"

// A relative --changed path is resolved against --project, from any working directory.
func TestRelativeChangedPathResolvesAgainstProject(t *testing.T) {
	root := admittedSkippingCheckout(t)
	outside := t.TempDir()

	t.Run("index", func(t *testing.T) {
		got := verilexIn(t, root, outside, "--project", root, "index", "--changed", "bin/tally")
		equal(t, got.code, 0)
		equal(t, got.stderr, "")
		equal(t, got.stdout, changedTally)
	})
	t.Run("files", func(t *testing.T) {
		got := verilexIn(t, root, outside, "--project", root, "index", "--json", "--changed", "bin/tally")
		equal(t, got.code, 0)
		var change struct {
			Files  []string `json:"files"`
			Claims []struct {
				Claim string `json:"claim"`
			} `json:"claims"`
		}
		if err := json.Unmarshal([]byte(got.stdout), &change); err != nil {
			t.Fatal(err)
		}
		equal(t, change.Files, []string{"bin/tally"})
		names := make([]string, len(change.Claims))
		for i, claim := range change.Claims {
			names[i] = claim.Claim
		}
		equal(t, names, []string{"item-added", "item-listed", "store-opened"})
	})
	t.Run("run", func(t *testing.T) {
		got := verilexIn(t, root, outside, "--project", root, "run", "--changed", "bin/tally")
		equal(t, got.code, 0)
		equal(t, got.stderr, "")
		if !strings.HasPrefix(got.stdout, "green: 3 green, skipped: stamps match run ") {
			t.Fatalf("stdout %q", got.stdout)
		}
	})
	t.Run("plan", func(t *testing.T) {
		got := verilexIn(t, root, outside, "--project", root, "plan", "--changed", "bin/tally")
		equal(t, got.code, 0)
		equal(t, got.stderr, "")
		if !strings.HasPrefix(got.stdout, "plan: skip 3, run 0;") {
			t.Fatalf("stdout %q", got.stdout)
		}
	})
	t.Run("absolute", func(t *testing.T) {
		got := verilexIn(t, root, outside, "--project", root, "index", "--changed", filepath.Join(root, "bin", "tally"))
		equal(t, got.code, 0)
		equal(t, got.stderr, "")
		equal(t, got.stdout, changedTally)
	})
}

// Bits git does not record leave the admitted digests and the proof stamps alone.
// 0700 still has the owner execute bit. 0654 has only the group execute bit.
// A directory mode is not recorded at all.
func TestUntrackedPermissionBitsKeepDigestsAndSkip(t *testing.T) {
	cases := []struct {
		name string
		path string
		mode os.FileMode
	}{
		{"file 0700", filepath.Join(".verilex", "words", "item-stored", "run"), 0o700},
		{"directory 0700", filepath.Join(".verilex", "words", "item-stored"), 0o700},
		{"group execute only", filepath.Join(".verilex", "words", "item-stored", "word.md"), 0o654},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := admittedSkippingCheckout(t)
			beforeRun := verilex(t, root, nil, "run", "--json", chain)
			equal(t, beforeRun.code, 0)
			beforeStamps := stampsOf(t, beforeRun.stdout)
			beforePlan := verilex(t, root, nil, "plan", chain).stdout
			if !strings.HasPrefix(beforePlan, "plan: skip 3, run 0\n") {
				t.Fatalf("baseline plan: %q", beforePlan)
			}
			if err := os.Chmod(filepath.Join(root, c.path), c.mode); err != nil {
				t.Fatal(err)
			}
			equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
			equal(t, verilex(t, root, nil, "plan", chain).stdout, beforePlan)
			afterRun := verilex(t, root, nil, "run", "--json", chain)
			equal(t, afterRun.code, 0)
			equal(t, stampsOf(t, afterRun.stdout), beforeStamps)
		})
	}
}

// Removing the owner execute bit from a file git recorded as executable changes the
// admission digest and the proof stamp. word.md starts at 0644, so the test first
// records it as executable and then returns it to 0644.
func TestRemovingOwnerExecuteInvalidatesAdmissionAndStamp(t *testing.T) {
	root := admittedSkippingCheckout(t)
	contract := filepath.Join(root, ".verilex", "words", "item-stored", "word.md")
	if err := os.Chmod(contract, 0o755); err != nil {
		t.Fatal(err)
	}
	admitted(t, root, "item-stored")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")

	live := verilex(t, root, nil, "run", "--json", chain)
	equal(t, live.code, 0)
	var rec runner.Record
	if err := json.Unmarshal([]byte(live.stdout), &rec); err != nil {
		t.Fatal(err)
	}
	equal(t, rec.Skipped, false)
	equal(t, rec.Rerun, "item-stored apple: word changed")

	skipped := verilex(t, root, nil, "plan", chain)
	if !strings.HasPrefix(skipped.stdout, "plan: skip 3, run 0\n") {
		t.Fatalf("plan after recording the executable contract: %q", skipped.stdout)
	}
	beforeRun := verilex(t, root, nil, "run", "--json", chain)
	equal(t, beforeRun.code, 0)
	before := stampNamed(t, stampsOf(t, beforeRun.stdout), "item-stored")

	if err := os.Chmod(contract, 0o644); err != nil {
		t.Fatal(err)
	}
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n  item-stored: the word's files changed since onboarding\n")
	removed := verilex(t, root, nil, "run", "--json", chain)
	equal(t, removed.code, 0)
	if err := json.Unmarshal([]byte(removed.stdout), &rec); err != nil {
		t.Fatal(err)
	}
	equal(t, rec.Skipped, false)
	after := stampNamed(t, stampsOf(t, removed.stdout), "item-stored")
	if after == before {
		t.Fatalf("item-stored stamp still %s", before)
	}
}

func stampNamed(t *testing.T, stamps []string, name string) string {
	t.Helper()
	prefix := name + "="
	for _, stamp := range stamps {
		if strings.HasPrefix(stamp, prefix) {
			return stamp
		}
	}
	t.Fatalf("no stamp for %s in %v", name, stamps)
	return ""
}

// A verilex from before RecordedPerm hashed every permission bit into the records it wrote. A
// team that onboarded and admitted its words with it on a group-writable checkout keeps those
// records after the upgrade while no file and no mode changes: the word files, the frame and the
// shared word files, for words that prove a claim, a word whose claim covers another claim's
// sentence, and a word without a claim. A mode change or an edit still drifts.
func TestFullModeRecordsOfGroupWritableCheckoutStayAdmitted(t *testing.T) {
	root := baseline(t)
	// item-kept-once maps a sentence of item-added's sub-feature, so item-stored stays clean only
	// while item-added-twice's onboarding decision matches its files.
	twice := "Expect exit 2 and `exists NAME` when NAME is already stored."
	edit(t, feature(root, "items.md"), "`store.json` lists NAME.\n", "`store.json` lists NAME. "+twice+"\n")
	write(t, claimFile(root, "item-kept-once"), "claim: item-kept-once\nsentence: A name is stored at most once.\nargs: [name]\nentry: [cli]\nrequires: [store]\n"+
		"evidence:\n  action: \"`tally add NAME` twice exits 2 the second time and prints `exists NAME`.\"\n  observation: store.json lists NAME once.\n"+
		"defects:\n  dropped-add: {TALLY_DEFECT: drop-adds}\n"+
		"sources:\n  - ref: "+itemAdd+"\n    prose: 2948a95bd310\n    requirements: [\""+twice+"\"]\n", 0644)
	dir := filepath.Join(root, ".verilex", "words", "item-added-twice")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "word.md"), "---\nword: item-added-twice\npromise: A name added twice is stored once.\nargs: [name]\nclaim: "+pinOf(t, root, "item-kept-once")+"\nentry: cli\ninputs: [bin/tally]\n---\n", 0644)
	write(t, filepath.Join(dir, "run"), read(t, filepath.Join(root, ".verilex", "words", "item-stored", "run")), 0755)
	used(t, root, "store-open | item-added-twice apple")
	used(t, root, "store-open | item-added-twice apple")
	onboard(t, root, "item-added-twice")
	addWord(t, root, "store-glanced", `echo '{"verdict": "pass", "observation": "fine"}'`+"\n")
	used(t, root, "store-open | store-glanced")
	used(t, root, "store-open | store-glanced")
	admit(t, root, "store-glanced")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (5 admitted)\n")

	groupWritable(t, filepath.Join(root, ".verilex"))
	fullModeRecords(t, root)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (5 admitted)\n")
	used(t, root, chain)
	if got := plan(t, root, chain).stdout; !strings.HasPrefix(got, "plan: skip 3, run 0\n") {
		t.Fatalf("plan: %q", got)
	}

	// The records hold every permission bit as it was judged, so any mode change drifts, a bit
	// git does not record (group write) or one it does (owner execute), and restoring recovers.
	stored := filepath.Join(root, ".verilex", "words", "item-stored", "run")
	chmod(t, stored, 0o755)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 2 of 5 admitted drift-suspect; they always run\n"+
		"  item-added-twice: claim item-kept-once needs review: verify-tally/features/items.md#item-add: requirement no claim maps: Expect exit 0 and `added NAME`; `store.json` lists NAME.\n"+
		"  item-stored: the word's files changed since onboarding\n")
	chmod(t, stored, 0o775)
	glanced := filepath.Join(root, ".verilex", "words", "store-glanced", "word.md")
	launch := filepath.Join(root, ".verilex", "frame", "launch")
	chmod(t, glanced, 0o764)
	chmod(t, launch, 0o755)
	frame := "the frame changed since onboarding (.verilex/frame)"
	equal(t, verilex(t, root, nil, "check").stdout, "check: 5 of 5 admitted drift-suspect; they always run\n"+
		"  item-added-twice: "+frame+"\n"+
		"  item-listed: "+frame+"\n"+
		"  item-stored: "+frame+"\n"+
		"  store-glanced: the word's files changed since admission; the frame changed since admission (.verilex/frame)\n"+
		"  store-open: "+frame+"\n")
	chmod(t, glanced, 0o664)
	chmod(t, launch, 0o775)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (5 admitted)\n")

	for _, word := range []string{"item-listed", "store-glanced"} {
		appendTo(t, filepath.Join(root, ".verilex", "words", word, "run"), "\n# changed\n")
	}
	equal(t, verilex(t, root, nil, "check").stdout, "check: 2 of 5 admitted drift-suspect; they always run\n"+
		"  item-listed: the word's files changed since onboarding\n"+
		"  store-glanced: the word's files changed since admission\n")
	appendTo(t, filepath.Join(root, ".verilex", "words", "tally_word.py"), "\n# changed\n")
	shared := "the shared word files changed since onboarding (.verilex/words/tally_word.py)"
	equal(t, verilex(t, root, nil, "check").stdout, "check: 5 of 5 admitted drift-suspect; they always run\n"+
		"  item-added-twice: "+shared+"\n"+
		"  item-listed: the word's files changed since onboarding; "+shared+"\n"+
		"  item-stored: "+shared+"\n"+
		"  store-glanced: the word's files changed since admission; the shared word files changed since admission (.verilex/words/tally_word.py)\n"+
		"  store-open: "+shared+"\n")
}

// A verilex from before RecordedPerm also hashed every permission bit into the packets it
// proposed. A team that proposed a word with it on a group-writable checkout admits the curator's
// verdict after the upgrade while no file and no mode changes. The admission records the digests
// verilex makes now, so the word stays admitted once the checkout has only the modes git records.
// A changed word file, mode, frame, shared word file or implemented section still refuses it.
func TestFullModePacketOfGroupWritableCheckoutIsAdmitted(t *testing.T) {
	root := baseline(t)
	addWord(t, root, "store-glanced", `echo '{"verdict": "pass", "observation": "fine"}'`+"\n")
	used(t, root, "store-open | store-glanced")
	used(t, root, "store-open | store-glanced")
	groupWritable(t, filepath.Join(root, ".verilex"))
	packet := fullModePacket(t, root, "store-glanced")
	file := verdict(t, root, `{"word": "store-glanced", "packet": "`+packet+`", "verdict": "admit", "curator": "curator-model-1", "reason": "One real moment, proven twice."}`)

	words := filepath.Join(root, ".verilex", "words")
	run := filepath.Join(words, "store-glanced", "run")
	contract := filepath.Join(words, "store-glanced", "word.md")
	for _, c := range []struct {
		name   string
		path   string
		change func(path string)
	}{
		{"word file", run, func(path string) { appendTo(t, path, "\n# changed\n") }},
		// The packet holds every permission bit as it was judged, a bit git does not record
		// (group write) and one it does (owner execute).
		{"group write", run, func(path string) { chmod(t, path, 0o755) }},
		{"owner execute", contract, func(path string) { chmod(t, path, 0o764) }},
		{"frame", filepath.Join(root, ".verilex", "frame", "launch"), func(path string) { appendTo(t, path, "\n# changed\n") }},
		{"shared word file", filepath.Join(words, "tally_word.py"), func(path string) { appendTo(t, path, "\n# changed\n") }},
		{"section", feature(root, "store.md"), func(path string) { appendTo(t, path, "- A changed gotcha.\n") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			info, err := os.Stat(c.path)
			if err != nil {
				t.Fatal(err)
			}
			text := read(t, c.path)
			c.change(c.path)
			equal(t, verilex(t, root, nil, "admit", "store-glanced", "--verdict", file), output{code: 2, stderr: "verilex: refused: store-glanced changed since packet " + packet + "; propose it again\n"})
			write(t, c.path, text, 0)
			chmod(t, c.path, info.Mode().Perm())
		})
	}

	equal(t, verilex(t, root, nil, "admit", "store-glanced", "--verdict", file), output{stdout: "admitted store-glanced (curator curator-model-1, 2 runs)\n"})
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (4 admitted)\n")
	chmodAll(t, filepath.Join(root, ".verilex"), func(perm fs.FileMode) fs.FileMode { return perm &^ 0o020 })
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (4 admitted)\n")
}

// fullModePacket proposes word and rewrites the packet's word digest and binding as a verilex
// from before RecordedPerm made them on this checkout. It returns the packet's id.
func fullModePacket(t *testing.T, root, word string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(root), "state", "tally", "proposals", word, propose(t, root, word).ID+".json")
	var packet curation.Packet
	if err := json.Unmarshal([]byte(read(t, path)), &packet); err != nil {
		t.Fatal(err)
	}
	project, err := dictionary.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	words, err := lifecycle.LoadWords(project)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(words, func(w dictionary.Word) bool { return w.Name == word })
	if i < 0 {
		t.Fatalf("no word %s", word)
	}
	packet.WordDigest, packet.Binding = fullModeWordDigest(t, words[i]), fullModeBinding(t, project)
	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(data), 0)
	return packet.ID
}

// fullModeRecords rewrites every onboarding decision and admission record as a verilex from
// before RecordedPerm wrote it on this checkout.
func fullModeRecords(t *testing.T, root string) {
	t.Helper()
	project, err := dictionary.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	words, err := lifecycle.LoadWords(project)
	if err != nil {
		t.Fatal(err)
	}
	g, err := grouping.Load(project)
	if err != nil {
		t.Fatal(err)
	}
	binding := fullModeBinding(t, project)
	for _, word := range words {
		if d, ok := g.Words[word.Name]; ok {
			d.Digest, d.Binding = fullModeWordDigest(t, word), binding
			g.Set(word.Name, d)
			continue
		}
		var a lifecycle.Admission
		if err = json.Unmarshal([]byte(read(t, lifecycle.AdmissionPath(word))), &a); err != nil {
			t.Fatal(err)
		}
		a.WordDigest, a.Binding = fullModeWordDigest(t, word), binding
		data, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		write(t, lifecycle.AdmissionPath(word), string(data), 0644)
	}
	if err = grouping.Save(project, g); err != nil {
		t.Fatal(err)
	}
}

// fullModeWordDigest is the word digest over every permission bit of each word file.
func fullModeWordDigest(t *testing.T, w dictionary.Word) string {
	t.Helper()
	h := sha256.New()
	err := lifecycle.EachFile(w, func(rel string, _ fs.FileMode, data []byte) {
		info, err := os.Stat(filepath.Join(w.Path, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h, "%s\x00%o\x00%d\x00", rel, info.Mode().Perm(), len(data))
		h.Write(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// fullModeBinding fingerprints the frame and the shared word files over every permission bit of
// each file and directory.
func fullModeBinding(t *testing.T, project dictionary.Project) map[string]string {
	t.Helper()
	parts, err := fingerprint.Binding(project)
	if err != nil {
		t.Fatal(err)
	}
	digests := map[string]string{}
	for _, part := range parts {
		lines := make([]string, len(part.Names))
		for i, path := range part.Paths() {
			lines[i] = part.Names[i] + "=" + fullModeTree(t, path)
		}
		digests[part.Name] = fingerprint.Of(strings.Join(lines, "\n"))
	}
	return digests
}

func fullModeTree(t *testing.T, root string) string {
	t.Helper()
	file := func(path string, info fs.FileInfo) string {
		sum := sha256.Sum256([]byte(fmt.Sprintf("file %o\n", info.Mode().Perm()) + read(t, path)))
		return hex.EncodeToString(sum[:])
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		return file(root, info)
	}
	var lines strings.Builder
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		switch {
		case err != nil:
			return err
		case info.IsDir() && fingerprint.Cache(entry.Name()):
			return filepath.SkipDir
		case info.IsDir():
			fmt.Fprintf(&lines, "%q dir %o\n", rel, info.Mode().Perm())
		default:
			fmt.Fprintf(&lines, "%q %s\n", rel, file(path, info))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint.Of(lines.String())
}
