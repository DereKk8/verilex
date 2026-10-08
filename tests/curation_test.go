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
	"github.com/DereKk8/verilex/internal/fingerprint"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/lifecycle"
)

const itemAdd = "verify-tally/features/items.md#item-add"

// addRequirement is the item-add requirement sentence the item-added claim maps to.
const addRequirement = "Expect exit 0 and `added NAME`; `store.json` lists NAME."

// reviewAdd is why item-added needs review once changeAddRequirement rewrites its requirement
// sentence: the sentence it maps is gone, and the new one is a requirement no claim maps yet.
const reviewAdd = "claim item-added needs review: " + itemAdd + ": requirement changed or gone: " + addRequirement +
	"; " + itemAdd + ": requirement no claim maps: Expect exit 0 and `stored NAME`; `store.json` lists NAME."

// changeAddRequirement rewrites the item-add requirement sentence in the verify skill.
func changeAddRequirement(t *testing.T, root string) {
	t.Helper()
	items := feature(root, "items.md")
	write(t, items, strings.Replace(read(t, items), "Expect exit 0 and `added NAME`", "Expect exit 0 and `stored NAME`", 1), 0644)
}

// withoutClaims rewrites tally's words into words that name feature-map sections instead of
// proving claims, the words the curator flow (propose and admit) still serves.
func withoutClaims(t *testing.T, root string) {
	t.Helper()
	contracts := map[string]string{
		"store-open":  "promise: A new, empty store is open and ready for items.\nrequires: []\nprovides: [store]\nimplements:\n  - verify-tally/features/store.md#store-open\n",
		"item-stored": "promise: A named item is in the store.\nargs: [name]\nrequires: [store]\nprovides: [\"item:{name}\"]\nimplements:\n  - " + itemAdd + "\n",
		"item-listed": "promise: A stored item shows up when a user lists the store.\nargs: [name]\nrequires: [\"item:{name}\"]\nprovides: []\nread_only: true\nimplements:\n  - verify-tally/features/items.md#item-list\n",
	}
	for word, contract := range contracts {
		write(t, filepath.Join(root, ".verilex", "words", word, "word.md"), "---\nword: "+word+"\n"+contract+"inputs: [bin/tally]\nenv: [TALLY_DEFECT, TALLY_SIMULATE_LOCK]\n---\n", 0644)
	}
}

// onboard runs onboarding for a word that must join the vocabulary.
func onboard(t *testing.T, root, word string, flags ...string) output {
	t.Helper()
	done := verilex(t, root, nil, append([]string{"onboard", word}, flags...)...)
	if done.code != 0 {
		t.Fatalf("onboard %s: %d\n%s%s", word, done.code, done.stdout, done.stderr)
	}
	return done
}

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
	withoutClaims(t, root)
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
	equal(t, packet.Sections[0].File, filepath.Join(".cursor", "skills", "verify-tally", "features", "items.md"))
	contains(t, packet.Sections[0].Text, "Expect exit 0 and `added NAME`")
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
	withoutClaims(t, root)
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
	withoutClaims(t, root)
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
	withoutClaims(t, root)
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
	onboard(t, root, "item-stored")
	onboard(t, root, "store-open")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (2 admitted)\n")

	items := feature(root, "items.md")
	original := read(t, items)
	changeAddRequirement(t, root)
	done := verilex(t, root, nil, "check")
	equal(t, done.code, 0)
	equal(t, done.stdout, "check: 1 of 2 admitted drift-suspect; they always run\n  item-stored: "+reviewAdd+"\n")
	equal(t, status(t, root, "item-stored"), "drift-suspect")
	equal(t, status(t, root, "store-open"), "admitted")

	write(t, items, strings.ReplaceAll(original, "`item-add`", "`item-put`"), 0644)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 2 admitted drift-suspect; they always run\n  item-stored: claim item-added needs review: "+itemAdd+": sub-feature item-add is gone\n")

	write(t, items, original, 0644)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (2 admitted)\n")

	run := filepath.Join(root, ".verilex", "words", "store-open", "run")
	write(t, run, read(t, run)+"\n", 0755)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 2 admitted drift-suspect; they always run\n  store-open: the word's files changed since onboarding\n")

	contains(t, onboard(t, root, "store-open").stdout, "onboarded store-open: claim "+pinOf(t, root, "store-opened")+" joins the vocabulary; caught dirty-open\n")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (2 admitted)\n")
}

// Rule: a word runs with more than its own directory: the helpers beside the word directories
// and the frame. Onboarding judged the word with them, so a change to either leaves every word
// drift-suspect until it is onboarded again. A feature-map line no claim cites, or config.yaml,
// changes neither.
func TestEditingSharedWordFilesOrFrameMarksEveryWordDriftSuspect(t *testing.T) {
	root := product(t)
	used(t, root, "store-open | item-stored apple")
	used(t, root, "store-open | item-stored pear")
	onboard(t, root, "item-stored")
	onboard(t, root, "store-open")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (2 admitted)\n")

	readme := feature(root, "README.md")
	original := read(t, readme)
	write(t, readme, original+"- Stores live under the run's own directory.\n", 0644)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (2 admitted)\n")
	write(t, readme, original, 0644)
	// config.yaml names the product, not what a word runs with: one vocabulary serves several products.
	config := filepath.Join(root, ".verilex", "config.yaml")
	original = read(t, config)
	write(t, config, original+"secret_patterns: ['NEVER-[0-9]+']\n", 0644)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (2 admitted)\n")
	write(t, config, original, 0644)

	shared := "the shared word files changed since onboarding (.verilex/words/tally_word.py)"
	helper := filepath.Join(root, ".verilex", "words", "tally_word.py")
	original = read(t, helper)
	write(t, helper, original+"\n# every result is a pass\n", 0644)
	done := verilex(t, root, nil, "check")
	equal(t, done.code, 0)
	equal(t, done.stdout, "check: 2 of 2 admitted drift-suspect; they always run\n  item-stored: "+shared+"\n  store-open: "+shared+"\n")
	used(t, root, "store-open | item-stored apple")
	equal(t, plan(t, root, "store-open | item-stored apple").stdout, "plan: skip 0, run 2; store-open: drift-suspect: "+shared+"\n")
	write(t, helper, original, 0644)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (2 admitted)\n")

	frame := "the frame changed since onboarding (.verilex/frame)"
	launch := filepath.Join(root, ".verilex", "frame", "launch")
	original = read(t, launch)
	write(t, launch, original+"\n# builds another checkout\n", 0755)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 2 of 2 admitted drift-suspect; they always run\n  item-stored: "+frame+"\n  store-open: "+frame+"\n")

	onboard(t, root, "store-open")
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 2 admitted drift-suspect; they always run\n  item-stored: "+frame+"\n")
	write(t, launch, original, 0755)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 2 admitted drift-suspect; they always run\n  store-open: "+frame+"\n")
}

// An interpreter cache beside the shared helpers is derived from them and differs between
// checkouts, so it drifts nothing.
func TestInterpreterCacheBesideSharedWordFilesDriftsNothing(t *testing.T) {
	root := product(t)
	used(t, root, "store-open")
	used(t, root, "store-open")
	onboard(t, root, "store-open")
	for _, cache := range []string{"words", "frame"} {
		dir := filepath.Join(root, ".verilex", cache, "__pycache__")
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, "helper.cpython-312.pyc"), "compiled\n", 0644)
	}
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (1 admitted)\n")
	write(t, filepath.Join(root, ".verilex", "words", "lib.py"), "# a new helper\n", 0644)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 1 admitted drift-suspect; they always run\n  store-open: the shared word files changed since onboarding (.verilex/words/lib.py, .verilex/words/tally_word.py)\n")
}

// Onboarding records the frame and the shared word files its trials ran with, so a change to
// them while it runs records nothing.
func TestBindingChangedDuringOnboardingRecordsNothing(t *testing.T) {
	root := product(t)
	used(t, root, "store-open")
	used(t, root, "store-open")
	launch := filepath.Join(root, ".verilex", "frame", "launch")
	original := read(t, launch)
	// Each launch leaves a line beside the word directories, as an edit during the trials would.
	write(t, launch, strings.Replace(original, "run = os.environ[\"VERILEX_RUN\"]\n", "run = os.environ[\"VERILEX_RUN\"]\nwith open(Path(os.environ[\"VERILEX_PROJECT_ROOT\"], \".verilex\", \"words\", \"launches.log\"), \"a\") as log:\n    log.write(run + \"\\n\")\n", 1), 0755)
	done := verilex(t, root, nil, "onboard", "store-open")
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: the frame or the shared word files changed while store-open was onboarded; onboard it again\n")
	equal(t, status(t, root, "store-open"), "provisional")
}

// A decision recorded before onboarding kept the frame and the shared word files cannot say the
// word still runs with them, so the word is drift-suspect until it is onboarded again.
func TestDecisionWithoutBindingIsDriftSuspect(t *testing.T) {
	root := product(t)
	used(t, root, "store-open")
	used(t, root, "store-open")
	onboard(t, root, "store-open")
	project, err := dictionary.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	g, err := grouping.Load(project)
	if err != nil {
		t.Fatal(err)
	}
	// Removing the binding by hand breaks the seal, so the older decision is sealed as onboarding sealed it.
	older := g.Words["store-open"]
	older.Binding = nil
	g.Set("store-open", older)
	if err = grouping.Save(project, g); err != nil {
		t.Fatal(err)
	}
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 1 admitted drift-suspect; they always run\n  store-open: the frame and the shared word files were not recorded at onboarding; onboard it again\n")
	onboard(t, root, "store-open")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (1 admitted)\n")
}

// Rule: an outside curator judged a word without a claim with the helpers and frame of its
// packet's time, so a change to either drifts the admitted word and voids a packet not yet judged.
func TestWordWithoutClaimDriftsWithSharedWordFilesAndFrame(t *testing.T) {
	root := product(t)
	addWord(t, root, "store-glanced", `echo '{"verdict": "pass", "observation": "fine"}'`+"\n")
	used(t, root, "store-open | store-glanced")
	used(t, root, "store-open | store-glanced")
	helper := filepath.Join(root, ".verilex", "words", "tally_word.py")
	original := read(t, helper)

	packet := propose(t, root, "store-glanced")
	write(t, helper, original+"\n# every result is a pass\n", 0644)
	done := verilex(t, root, nil, "admit", "store-glanced", "--verdict", verdict(t, root, `{"word": "store-glanced", "packet": "`+packet.ID+`", "verdict": "admit", "curator": "m"}`))
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: store-glanced changed since packet "+packet.ID+"; propose it again\n")
	equal(t, status(t, root, "store-glanced"), "provisional")

	write(t, helper, original, 0644)
	admit(t, root, "store-glanced")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (1 admitted)\n")
	write(t, helper, original+"\n# every result is a pass\n", 0644)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 1 admitted drift-suspect; they always run\n  store-glanced: the shared word files changed since admission (.verilex/words/tally_word.py)\n")
	write(t, helper, original, 0644)
	cleanup := filepath.Join(root, ".verilex", "frame", "cleanup")
	write(t, cleanup, read(t, cleanup)+"\n# keeps the store\n", 0755)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 1 admitted drift-suspect; they always run\n  store-glanced: the frame changed since admission (.verilex/frame)\n")
}

// A word without a claim keeps the older anchor: its whole feature-map section, or the whole
// file for a sub-feature id.
func TestWordWithoutClaimDriftsWithItsWholeSection(t *testing.T) {
	root := product(t)
	addWord(t, root, "store-glanced", `echo '{"verdict": "pass", "observation": "fine"}'`+"\n")
	used(t, root, "store-open | store-glanced")
	used(t, root, "store-open | store-glanced")
	packet := admit(t, root, "store-glanced")
	equal(t, len(packet.Sections), 1)
	contains(t, packet.Sections[0].Text, "`store-open` opens an empty store.")
	equal(t, status(t, root, "store-glanced"), "admitted")

	store := feature(root, "store.md")
	write(t, store, read(t, store)+"- Store names are case-sensitive.\n", 0644)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 1 admitted drift-suspect; they always run\n  store-glanced: verify-tally/features/store.md#store-open: section changed\n")
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

// curated copies tally with every word admitted, standing in for the onboarding and curator flows
// tested elsewhere, so tests about skipping start from words that are allowed to skip.
func curated(t *testing.T) string {
	t.Helper()
	root := product(t)
	admitted(t, root)
	return root
}

// admitted records each named word (every word when none is named) as admitted, matching its files,
// the frame, the shared word files and its claim version as they are now: a sealed onboarding decision for a word that proves a claim,
// an admission record for one that names feature-map sections.
func admitted(t *testing.T, root string, names ...string) {
	t.Helper()
	admittedFor(t, root, "tally", names...)
}

// admittedFor records words as admitted with their uses in another product.
func admittedFor(t *testing.T, root, product string, names ...string) {
	t.Helper()
	project, err := dictionary.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	words, err := lifecycle.LoadWords(project)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := fingerprint.Digests(project)
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
		if word.Claim != nil {
			g, err := grouping.Load(project)
			if err != nil {
				t.Fatal(err)
			}
			d := grouping.Decision{Claim: word.Claim.Pin(), Entry: word.Entry, Digest: digest, Binding: binding, Match: grouping.Same, Defects: map[string]string{},
				Uses: map[string][]string{product: {"1-a", "2-b"}}, ClaimSources: word.Claim.SourcesDigest(), Date: time.Now().UTC().Format(time.RFC3339), Record: "test"}
			for name, defect := range word.Claim.Defects {
				d.Defects[name] = defect.Digest()
			}
			g.Set(word.Name, d)
			if err = grouping.Save(project, g); err != nil {
				t.Fatal(err)
			}
		} else {
			admission := lifecycle.Admission{Word: word.Name, Date: time.Now().UTC().Format(time.RFC3339), Curator: "test-curator", Packet: "0123456789abcdef", Runs: []string{"1-a", "2-b"}, WordDigest: digest, Binding: binding, Sections: map[string]string{}}
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
		}
		if s := status(t, root, word.Name); s != "admitted" {
			t.Fatalf("%s is %s after admission", word.Name, s)
		}
	}
}

// unadmitted drops a word's onboarding decision, standing in for a word that was never onboarded.
func unadmitted(t *testing.T, root, word string) {
	t.Helper()
	project, err := dictionary.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	g, err := grouping.Load(project)
	if err != nil {
		t.Fatal(err)
	}
	delete(g.Words, word)
	if err = grouping.Save(project, g); err != nil {
		t.Fatal(err)
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
	equal(t, proof.Rerun, "item-listed apple: word admission changed")
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
	changeAddRequirement(t, root)
	equal(t, status(t, root, "item-stored"), "drift-suspect")
	for i := 0; i < 2; i++ {
		record := green(t, root, nil, chain)
		equal(t, record.Rerun, "item-stored apple: drift-suspect: "+reviewAdd)
		ranLive(t, record)
		equal(t, record.Words[1].Stamp, first.Words[1].Stamp)
	}
	write(t, items, strings.ReplaceAll(original, "`item-add`", "`item-put`"), 0644)
	record := green(t, root, nil, chain)
	equal(t, record.Rerun, "item-stored apple: drift-suspect: claim item-added needs review: "+itemAdd+": sub-feature item-add is gone")
	ranLive(t, record)

	// A grouping file that cannot be read leaves every word that proves a claim without a stamp.
	write(t, items, original, 0644)
	skipped := green(t, root, nil, chain)
	equal(t, skipped.Skipped, true)
	groupingFile := filepath.Join(root, ".verilex", "grouping.yaml")
	saved := read(t, groupingFile)
	write(t, groupingFile, "words: [not, a, mapping\n", 0644)
	for i := 0; i < 2; i++ {
		record = green(t, root, nil, chain)
		contains(t, record.Rerun, "store-open: grouping: "+groupingFile+": malformed grouping file: ")
		ranLive(t, record)
	}

	// Restoring the file lifts the hold. The file is part of the word's stamp, so the stamps are
	// again those of the passes recorded before it became unreadable, and those still stand.
	write(t, groupingFile, saved, 0644)
	record = green(t, root, nil, chain)
	equal(t, record.Skipped, true)
	equal(t, record.Words[2].ReliesOn, skipped.Words[2].ReliesOn)
}

// Rule: a use is a green or red step in a live run that was itself green or red. Inconclusive
// steps, inconclusive runs and skipped runs never drove the product to a verdict.
func TestOnlyJudgedLiveRunsCountAsUses(t *testing.T) {
	root := curated(t)
	first := green(t, root, nil, chain)
	equal(t, green(t, root, nil, chain).Skipped, true)
	runJSON(t, root, map[string]string{"TALLY_SIMULATE_LOCK": "1"}, chain)
	cleanup := filepath.Join(root, ".verilex", "frame", "cleanup")
	appendTo(t, cleanup, "\nif os.environ.get(\"LEAK\"):\n    (Path(os.environ[\"VERILEX_EVIDENCE\"]) / \"key.pem\").write_text(\"-----BEGIN PRIVATE KEY-----\\n\")\n")
	done, leaked := runJSON(t, root, map[string]string{"LEAK": "1"}, chain)
	equal(t, done.code, 2)
	equal(t, verdicts(leaked), []string{"green", "green", "green"})

	write(t, filepath.Join(root, ".verilex", "words", "item-stored", "word.md"), read(t, filepath.Join(root, ".verilex", "words", "item-stored", "word.md"))+"More words.\n", 0644)
	equal(t, status(t, root, "item-stored"), "drift-suspect")
	done = verilex(t, root, nil, "onboard", "item-stored")
	equal(t, done.code, 2)
	contains(t, done.stderr, "item-stored has counted uses in 1 run(s) of tally")

	_, red := runJSON(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, chain)
	equal(t, verdicts(red), []string{"green", "red"})
	onboard(t, root, "item-stored")
	equal(t, decision(t, root, "item-stored").Uses["tally"], []string{first.Run, red.Run})
}
