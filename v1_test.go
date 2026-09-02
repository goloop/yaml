package yaml

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// --- types this package builds from a scalar itself ---

func TestDurationFromString(t *testing.T) {
	var c struct {
		Timeout time.Duration `yaml:"timeout"`
		Idle    time.Duration `yaml:"idle"`
	}
	err := Unmarshal([]byte("timeout: 30s\nidle: 1h30m\n"), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Timeout != 30*time.Second {
		t.Errorf("timeout = %v", c.Timeout)
	}
	if c.Idle != 90*time.Minute {
		t.Errorf("idle = %v", c.Idle)
	}
}

func TestDurationRefusesBareNumber(t *testing.T) {
	var c struct {
		Timeout time.Duration `yaml:"timeout"`
	}
	err := Unmarshal([]byte("timeout: 30\n"), &c)
	if err == nil {
		t.Fatal("a bare number was read as nanoseconds")
	}
	if !strings.Contains(err.Error(), `"30s"`) {
		t.Errorf("the error does not say how to write it: %v", err)
	}
	var te *TypeError
	if !errors.As(err, &te) || te.Line != 1 {
		t.Errorf("want a TypeError on line 1, got %v", err)
	}
}

func TestDurationRoundTrip(t *testing.T) {
	type conf struct {
		Timeout time.Duration `yaml:"timeout"`
	}
	out, err := MarshalString(conf{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if out != "timeout: 30s\n" {
		t.Fatalf("got %q", out)
	}
	var back conf
	if err := UnmarshalString(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.Timeout != 30*time.Second {
		t.Errorf("round trip lost the duration: %v", back.Timeout)
	}
}

func TestTimeLayout(t *testing.T) {
	type conf struct {
		Since time.Time `yaml:"since" layout:"DateOnly"`
		At    time.Time `yaml:"at"`
	}
	var c conf
	src := "since: 2026-09-01\nat: 2026-09-01T10:00:00Z\n"
	if err := Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	if c.Since.Format(time.DateOnly) != "2026-09-01" {
		t.Errorf("since = %v", c.Since)
	}
	if c.At.Hour() != 10 {
		t.Errorf("at = %v", c.At)
	}

	out, err := MarshalString(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "since: 2026-09-01\n") {
		t.Errorf("the layout tag did not reach the encoder: %q", out)
	}
}

func TestTimeLayoutOption(t *testing.T) {
	var c struct {
		At time.Time `yaml:"at"`
	}
	err := Unmarshal([]byte("at: 2026-09-01\n"), &c, WithTimeLayout("DateOnly"))
	if err != nil {
		t.Fatal(err)
	}
	if c.At.Year() != 2026 {
		t.Errorf("at = %v", c.At)
	}
}

func TestURL(t *testing.T) {
	type conf struct {
		Addr     url.URL  `yaml:"addr"`
		Callback *url.URL `yaml:"callback"`
	}
	var c conf
	src := "addr: https://example.com/x?q=1\ncallback: /local/path\n"
	if err := Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	if c.Addr.Host != "example.com" || c.Addr.Path != "/x" {
		t.Errorf("addr = %+v", c.Addr)
	}
	// A relative reference is a URL and stays one.
	if c.Callback == nil || c.Callback.Path != "/local/path" {
		t.Errorf("callback = %+v", c.Callback)
	}

	out, err := MarshalString(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "addr: https://example.com/x?q=1") {
		t.Fatalf("the URL was not written as text: %q", out)
	}
	if strings.Contains(out, "rawpath") || strings.Contains(out, "omithost") {
		t.Fatalf("net/url internals leaked into the document: %q", out)
	}
}

// --- def and required ---

func TestDefaults(t *testing.T) {
	type conf struct {
		Host    string        `yaml:"host" def:"localhost"`
		Port    int           `yaml:"port" def:"8080"`
		Timeout time.Duration `yaml:"timeout" def:"30s"`
		Ptr     *int          `yaml:"ptr" def:"7"`
	}
	var c conf
	if err := Unmarshal([]byte("host: example.com\n"), &c); err != nil {
		t.Fatal(err)
	}
	if c.Host != "example.com" {
		t.Errorf("the document lost to the default: %q", c.Host)
	}
	if c.Port != 8080 {
		t.Errorf("port = %d", c.Port)
	}
	if c.Timeout != 30*time.Second {
		t.Errorf("timeout = %v", c.Timeout)
	}
	if c.Ptr == nil || *c.Ptr != 7 {
		t.Errorf("a pointer default was not allocated: %v", c.Ptr)
	}
}

func TestDefaultDoesNotOverrideExplicitNull(t *testing.T) {
	type conf struct {
		Port int  `yaml:"port" def:"8080"`
		Ptr  *int `yaml:"ptr" def:"7"`
	}
	var c conf
	if err := Unmarshal([]byte("port: null\nptr: null\n"), &c); err != nil {
		t.Fatal(err)
	}
	// Writing null is an act; a decoder that undoes it is overruling the
	// person who wrote the file.
	if c.Port != 0 {
		t.Errorf("null was replaced by the default: %d", c.Port)
	}
	if c.Ptr != nil {
		t.Errorf("null was replaced by the default: %v", c.Ptr)
	}
}

func TestRequired(t *testing.T) {
	type conf struct {
		Secret string `yaml:"secret,required"`
		Host   string `yaml:"host"`
	}

	t.Run("absent key fails", func(t *testing.T) {
		var c conf
		err := Unmarshal([]byte("host: x\n"), &c)
		if !errors.Is(err, ErrRequired) {
			t.Fatalf("want ErrRequired, got %v", err)
		}
		var te *TypeError
		if !errors.As(err, &te) {
			t.Fatalf("the position was lost: %v", err)
		}
	})

	t.Run("an explicit null satisfies presence", func(t *testing.T) {
		var c conf
		if err := Unmarshal([]byte("secret: null\n"), &c); err != nil {
			t.Fatalf("required asked for more than presence: %v", err)
		}
	})

	t.Run("a merge key counts as present", func(t *testing.T) {
		var c conf
		src := "defaults: &d\n  secret: abc\nservice:\n  <<: *d\n"
		var doc struct {
			Service conf `yaml:"service"`
		}
		if err := Unmarshal([]byte(src), &doc, WithStrict()); err == nil {
			if doc.Service.Secret != "abc" {
				t.Errorf("merge did not fill the field: %+v", doc.Service)
			}
		} else if !strings.Contains(err.Error(), "unknown key \"defaults\"") {
			t.Fatalf("unexpected error: %v", err)
		}
		_ = c
	})
}

func TestRequiredAfterMerge(t *testing.T) {
	type service struct {
		Token string `yaml:"token,required"`
	}
	var doc struct {
		Defaults map[string]string `yaml:"defaults"`
		Service  service           `yaml:"service"`
	}
	src := "defaults: &d\n  token: abc\nservice:\n  <<: *d\n"
	if err := Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("required did not see the merged key: %v", err)
	}
	if doc.Service.Token != "abc" {
		t.Errorf("token = %q", doc.Service.Token)
	}
}

func TestRequiredAllSkipsSections(t *testing.T) {
	type tls struct {
		Cert string `yaml:"cert"`
		Key  string `yaml:"key"`
	}
	type conf struct {
		Host string `yaml:"host"`
		TLS  tls    `yaml:"tls"`
	}
	var c conf
	// The section is absent, so it is not required; if it were, every
	// optional section would make a config unloadable.
	if err := Unmarshal([]byte("host: x\n"), &c, WithRequiredAll()); err != nil {
		t.Fatalf("an absent section was treated as a required leaf: %v", err)
	}
	var c2 conf
	err := Unmarshal([]byte("host: x\ntls:\n  cert: c\n"), &c2, WithRequiredAll())
	if !errors.Is(err, ErrRequired) {
		t.Fatalf("a leaf of a present section was not required: %v", err)
	}
}

func TestRequiredAndDefContradict(t *testing.T) {
	var c struct {
		X string `yaml:"x,required" def:"y"`
	}
	err := Unmarshal([]byte("x: z\n"), &c)
	if err == nil || !strings.Contains(err.Error(), "contradict") {
		t.Fatalf("the contradiction was accepted: %v", err)
	}
}

// --- expansion ---

func TestExpand(t *testing.T) {
	t.Setenv("HOST", "example.com")
	t.Setenv("PORT", "8080")

	var c struct {
		URL  string `yaml:"url"`
		Port int    `yaml:"port"`
		Bare string `yaml:"bare"`
	}
	src := "url: https://${HOST}/a\nport: ${PORT}\nbare: $HOST\n"
	if err := Unmarshal([]byte(src), &c, WithExpand()); err != nil {
		t.Fatal(err)
	}
	if c.URL != "https://example.com/a" {
		t.Errorf("url = %q", c.URL)
	}
	// Expansion runs before the scalar is typed, so a number that came
	// from the environment is still a number.
	if c.Port != 8080 {
		t.Errorf("port = %d", c.Port)
	}
	if c.Bare != "example.com" {
		t.Errorf("bare = %q", c.Bare)
	}
}

func TestExpandOffByDefault(t *testing.T) {
	t.Setenv("HOST", "example.com")
	var c struct {
		URL string `yaml:"url"`
	}
	if err := Unmarshal([]byte("url: ${HOST}\n"), &c); err != nil {
		t.Fatal(err)
	}
	if c.URL != "${HOST}" {
		t.Errorf("expansion happened without being asked for: %q", c.URL)
	}
}

// TestExpandKeepsDollarText is the reason this package does not use the
// shell's substitution language: a config file has no positional
// parameters, but it does have prices and one-liners.
func TestExpandKeepsDollarText(t *testing.T) {
	var c struct {
		Price string `yaml:"price"`
		Awk   string `yaml:"awk"`
		Cost  string `yaml:"cost"`
		Twice string `yaml:"twice"`
		Alone string `yaml:"alone"`
	}
	src := "price: \"cost: $100\"\nawk: \"$1 == x\"\ncost: $5\n" +
		"twice: a$$b\nalone: \"100$\"\n"
	if err := Unmarshal([]byte(src), &c, WithExpand()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ got, want string }{
		{c.Price, "cost: $100"},
		{c.Awk, "$1 == x"},
		{c.Cost, "$5"},
		{c.Twice, "a$$b"},
		{c.Alone, "100$"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestExpandStyles(t *testing.T) {
	t.Setenv("HOST", "example.com")
	var c struct {
		Plain   string `yaml:"plain"`
		Double  string `yaml:"double"`
		Single  string `yaml:"single"`
		Literal string `yaml:"literal"`
		Folded  string `yaml:"folded"`
	}
	src := "plain: ${HOST}\ndouble: \"${HOST}\"\nsingle: '${HOST}'\n" +
		"literal: |\n  ${HOST}\nfolded: >\n  ${HOST}\n"
	if err := Unmarshal([]byte(src), &c, WithExpand()); err != nil {
		t.Fatal(err)
	}
	if c.Plain != "example.com" || c.Double != "example.com" {
		t.Errorf("plain=%q double=%q", c.Plain, c.Double)
	}
	if c.Single != "${HOST}" {
		t.Errorf("single quotes are the literal form: %q", c.Single)
	}
	if strings.TrimSpace(c.Literal) != "${HOST}" {
		t.Errorf("a block scalar is literal: %q", c.Literal)
	}
	if strings.TrimSpace(c.Folded) != "${HOST}" {
		t.Errorf("a folded scalar is literal: %q", c.Folded)
	}
}

func TestExpandInFlowCollections(t *testing.T) {
	t.Setenv("HOST", "example.com")
	var c struct {
		URLs []string          `yaml:"urls"`
		Env  map[string]string `yaml:"env"`
	}
	// The braced form has to be quoted here, and that is YAML, not this
	// package: "{" and "}" are flow indicators, so a plain scalar inside
	// a flow collection cannot contain "${...}" at all. The bare form
	// needs no quoting, which is one reason to support it.
	src := "urls: [\"https://${HOST}/a\", '${HOST}', $HOST]\n" +
		"env: { host: $HOST, literal: '${HOST}' }\n"
	if err := Unmarshal([]byte(src), &c, WithExpand()); err != nil {
		t.Fatal(err)
	}
	want := []string{"https://example.com/a", "${HOST}", "example.com"}
	for i, w := range want {
		if c.URLs[i] != w {
			t.Errorf("urls[%d] = %q, want %q", i, c.URLs[i], w)
		}
	}
	if c.Env["host"] != "example.com" || c.Env["literal"] != "${HOST}" {
		t.Errorf("env = %v", c.Env)
	}
}

// TestBracesAreFlowIndicators pins the rule the test above works
// around, so nobody later reads it as an expansion bug.
func TestBracesAreFlowIndicators(t *testing.T) {
	var m map[string]any
	err := Unmarshal([]byte("urls: [https://${HOST}/a]\n"), &m)
	if err == nil {
		t.Fatal("a plain flow scalar took a brace")
	}
	// Quoting it, or writing it in block context, is fine.
	if err := Unmarshal([]byte("urls:\n  - https://${HOST}/a\n"), &m); err != nil {
		t.Fatalf("block context: %v", err)
	}
}

func TestExpandNeverTouchesKeys(t *testing.T) {
	t.Setenv("KEY", "host")
	var m map[string]string
	if err := Unmarshal([]byte("${KEY}: x\n"), &m, WithExpand()); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["${KEY}"]; !ok {
		t.Errorf("a key was expanded: %v", m)
	}
}

func TestExpandCannotChangeStructure(t *testing.T) {
	// The value is substituted into one scalar, never into the document
	// text, so a colon or a newline in a variable cannot add a key.
	t.Setenv("EVIL", "a: 1\nb: 2")
	var m map[string]string
	if err := Unmarshal([]byte("value: ${EVIL}\n"), &m, WithExpand()); err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m["value"] != "a: 1\nb: 2" {
		t.Fatalf("expansion changed the shape of the document: %v", m)
	}
}

func TestExpandUndefined(t *testing.T) {
	os.Unsetenv("DEFINITELY_NOT_SET")

	t.Run("lax leaves it empty", func(t *testing.T) {
		var c struct {
			X string `yaml:"x"`
		}
		if err := Unmarshal([]byte("x: ${DEFINITELY_NOT_SET}\n"), &c,
			WithExpand()); err != nil {
			t.Fatal(err)
		}
		if c.X != "" {
			t.Errorf("x = %q", c.X)
		}
	})

	t.Run("strict names the variable and the line", func(t *testing.T) {
		var c struct {
			X string `yaml:"x"`
		}
		err := Unmarshal([]byte("a: 1\nx: ${DEFINITELY_NOT_SET}\n"), &c,
			WithExpandStrict())
		if err == nil {
			t.Fatal("an undefined variable passed")
		}
		if !strings.Contains(err.Error(), "DEFINITELY_NOT_SET") {
			t.Errorf("the error does not name the variable: %v", err)
		}
		var te *TypeError
		if !errors.As(err, &te) || te.Line != 2 {
			t.Errorf("want line 2, got %v", err)
		}
	})
}

func TestExpandMalformedReference(t *testing.T) {
	var c struct {
		X string `yaml:"x"`
	}
	err := Unmarshal([]byte("x: \"${HOST\"\n"), &c, WithExpand())
	if err == nil {
		t.Fatal("an unterminated reference was taken as text")
	}
}

func TestDefaultIsExpanded(t *testing.T) {
	t.Setenv("TIMEOUT", "45s")
	var c struct {
		Timeout time.Duration `yaml:"timeout" def:"${TIMEOUT}"`
	}
	if err := Unmarshal([]byte("other: 1\n"), &c, WithExpand()); err != nil {
		t.Fatal(err)
	}
	if c.Timeout != 45*time.Second {
		t.Errorf("a default was not expanded: %v", c.Timeout)
	}
}

// --- hooks ---

type level struct{ n int }

func (l *level) UnmarshalYAML(decode func(any) error) error {
	var s string
	if err := decode(&s); err == nil {
		switch s {
		case "low":
			l.n = 1
			return nil
		case "high":
			l.n = 9
			return nil
		}
	}
	var n int
	if err := decode(&n); err != nil {
		return err
	}
	l.n = n
	return nil
}

func (l level) MarshalYAML() (any, error) {
	switch l.n {
	case 1:
		return "low", nil
	case 9:
		return "high", nil
	}
	return l.n, nil
}

func TestHooks(t *testing.T) {
	type conf struct {
		A level `yaml:"a"`
		B level `yaml:"b"`
	}
	var c conf
	if err := Unmarshal([]byte("a: high\nb: 4\n"), &c); err != nil {
		t.Fatal(err)
	}
	if c.A.n != 9 || c.B.n != 4 {
		t.Fatalf("got %+v", c)
	}

	out, err := MarshalString(conf{A: level{1}, B: level{4}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "a: low\nb: 4\n" {
		t.Fatalf("got %q", out)
	}
}

type loop struct{}

func (l loop) MarshalYAML() (any, error) { return l, nil }

func TestMarshalerLoopIsAnError(t *testing.T) {
	_, err := Marshal(loop{})
	if err == nil {
		t.Fatal("a self-returning MarshalYAML did not stop")
	}
	if !strings.Contains(err.Error(), "MarshalYAML") {
		t.Errorf("the error does not name the cause: %v", err)
	}
}

type failing struct{}

func (f *failing) UnmarshalYAML(decode func(any) error) error {
	return errors.New("no")
}

func TestHookErrorKeepsTheLine(t *testing.T) {
	var c struct {
		A int      `yaml:"a"`
		B *failing `yaml:"b"`
	}
	err := Unmarshal([]byte("a: 1\nb: x\n"), &c)
	var te *TypeError
	if !errors.As(err, &te) || te.Line != 2 {
		t.Fatalf("want a TypeError on line 2, got %v", err)
	}
}

// --- registered parsers and encoders ---

type money int

func TestWithParserAndEncoder(t *testing.T) {
	type conf struct {
		Price money   `yaml:"price"`
		List  []money `yaml:"list"`
		Ptr   *money  `yaml:"ptr"`
	}
	parse := WithParser(func(s string) (money, error) {
		if !strings.HasPrefix(s, "$") {
			return 0, errors.New("a price starts with $")
		}
		n := 0
		for _, r := range s[1:] {
			n = n*10 + int(r-'0')
		}
		return money(n), nil
	})

	var c conf
	src := "price: $12\nlist: [$1, $2]\nptr: $9\n"
	if err := Unmarshal([]byte(src), &c, parse); err != nil {
		t.Fatal(err)
	}
	if c.Price != 12 || len(c.List) != 2 || c.List[1] != 2 {
		t.Fatalf("got %+v", c)
	}
	if c.Ptr == nil || *c.Ptr != 9 {
		t.Fatalf("pointer element: %v", c.Ptr)
	}

	out, err := MarshalString(c, WithEncoder(func(m money) (string, error) {
		return "$" + strings.TrimSpace(strings.Repeat("", 0)) +
			itoa(int(m)), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "price: $12") {
		t.Fatalf("got %q", out)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// --- shape of the API ---

func TestIndentOption(t *testing.T) {
	v := map[string]any{"a": map[string]any{"b": 1}}
	out, err := MarshalString(v, WithIndent(4))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\n    b: 1") {
		t.Fatalf("got %q", out)
	}
}

func TestFileRoundTrip(t *testing.T) {
	type conf struct {
		Host string        `yaml:"host"`
		Wait time.Duration `yaml:"wait"`
	}
	path := t.TempDir() + "/cfg.yaml"
	want := conf{Host: "example.com", Wait: 5 * time.Second}
	if err := MarshalFile(path, want, WithFileMode(0o600)); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	var got conf
	if err := UnmarshalFile(path, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestReaderWriter(t *testing.T) {
	var buf strings.Builder
	if err := MarshalWriter(&buf, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	var m map[string]int
	if err := UnmarshalReader(strings.NewReader(buf.String()), &m); err != nil {
		t.Fatal(err)
	}
	if m["a"] != 1 {
		t.Errorf("got %v", m)
	}
}

func TestSentinels(t *testing.T) {
	if err := Unmarshal([]byte("a: 1\n"), nil); !errors.Is(err, ErrNilObject) {
		t.Errorf("nil: %v", err)
	}
	var m map[string]int
	if err := Unmarshal([]byte("a: 1\n"), m); !errors.Is(err, ErrNotPointer) {
		t.Errorf("non-pointer: %v", err)
	}
}

// TestStrictCatchesForeignCommentSyntax records why an unknown key is
// worth refusing: "//" is legal plain scalar text in YAML, so a document
// written in another language's comment style parses cleanly and means
// something nobody intended.
func TestStrictCatchesForeignCommentSyntax(t *testing.T) {
	var c struct {
		Name string `yaml:"name"`
		Port int    `yaml:"port"`
	}
	src := "{\n  // service name\n  \"name\": \"app\",\n  \"port\": 8080\n}"

	if err := Unmarshal([]byte(src), &c); err != nil {
		t.Fatalf("the document is valid YAML: %v", err)
	}
	if c.Name != "" {
		t.Errorf("name = %q", c.Name)
	}

	err := Unmarshal([]byte(src), &c, WithStrict())
	if err == nil {
		t.Fatal("strict decoding accepted the foreign key")
	}
	if !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("got %v", err)
	}
}

func TestSlashesStayInScalars(t *testing.T) {
	var m map[string]string
	src := "share: //fileserver/pub\nurl: https://example.com/a//b\n"
	if err := Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if m["share"] != "//fileserver/pub" {
		t.Errorf("share = %q", m["share"])
	}
	if m["url"] != "https://example.com/a//b" {
		t.Errorf("url = %q", m["url"])
	}
}
