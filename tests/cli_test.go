package backend_test

import (
	"reflect"
	"testing"

	"xwdtt/cli"
)

// ── ParseWdttLink: wdtt://IP:ДТЛС:WG:ПРОКСИ:ПАРОЛЬ[:ХЕШИ][#название] ──

func TestParseWdttLink_Legacy_Basic(t *testing.T) {
	l, ok := cli.ParseWdttLink("wdtt://1.2.3.4:8443:51820:1080:pass")
	if !ok {
		t.Fatal("ожидался успешный разбор")
	}
	if l.Host != "1.2.3.4:8443" {
		t.Errorf("Host = %q, ожидалось 1.2.3.4:8443", l.Host)
	}
	if l.Password != "pass" {
		t.Errorf("Password = %q, ожидалось pass", l.Password)
	}
	if len(l.Hashes) != 0 {
		t.Errorf("Hashes = %v, ожидалось пусто", l.Hashes)
	}
}

func TestParseWdttLink_Legacy_HashesAndName(t *testing.T) {
	l, ok := cli.ParseWdttLink("wdtt://1.2.3.4:8443:51820:1080:pass:h1,h2#Мой сервер")
	if !ok {
		t.Fatal("ожидался успешный разбор")
	}
	if l.Password != "pass" {
		t.Errorf("Password = %q, ожидалось pass", l.Password)
	}
	if !reflect.DeepEqual(l.Hashes, []string{"h1", "h2"}) {
		t.Errorf("Hashes = %v, ожидалось [h1 h2]", l.Hashes)
	}
	if l.Name != "Мой сервер" {
		t.Errorf("Name = %q, ожидалось «Мой сервер»", l.Name)
	}
}

func TestParseWdttLink_Legacy_PasswordWithColonNoHashes(t *testing.T) {
	// Последний «:» в хвосте — разделитель password/hashes (как в wdttLink.ts):
	// колон внутри пароля формат не различает, хвост уходит в хеши.
	l, ok := cli.ParseWdttLink("wdtt://1.2.3.4:8443:51820:1080:my:pass#S")
	if !ok {
		t.Fatal("ожидался успешный разбор")
	}
	if l.Password != "my" {
		t.Errorf("Password = %q, ожидалось my", l.Password)
	}
	if !reflect.DeepEqual(l.Hashes, []string{"pass"}) {
		t.Errorf("Hashes = %v, ожидалось [pass]", l.Hashes)
	}
	if l.Name != "S" {
		t.Errorf("Name = %q, ожидалось S", l.Name)
	}
}

func TestParseWdttLink_Qwdtt(t *testing.T) {
	l, ok := cli.ParseWdttLink("qwdtt://config?name=Base&peer=5.6.7.8:9999&pass=pw&hashes=a,b&workers=18&port=3478")
	if !ok {
		t.Fatal("ожидался успешный разбор")
	}
	if l.Name != "Base" || l.Host != "5.6.7.8:9999" || l.Password != "pw" {
		t.Errorf("разобрано неверно: %+v", l)
	}
	if !reflect.DeepEqual(l.Hashes, []string{"a", "b"}) {
		t.Errorf("Hashes = %v, ожидалось [a b]", l.Hashes)
	}
	if l.Workers != 18 || l.Port != "3478" {
		t.Errorf("Workers=%d Port=%q, ожидалось 18/3478", l.Workers, l.Port)
	}
}

func TestParseWdttLink_Qwdtt_NoPassword(t *testing.T) {
	if _, ok := cli.ParseWdttLink("qwdtt://config?peer=1.2.3.4:1"); ok {
		t.Fatal("ссылка без пароля должна отвергаться")
	}
}

func TestParseWdttLink_Invalid(t *testing.T) {
	for _, s := range []string{
		"not-a-link",
		"wdtt://1.2.3.4:8443", // слишком мало частей
		"wdtt://",             // пусто
		"",
	} {
		if _, ok := cli.ParseWdttLink(s); ok {
			t.Errorf("ссылка %q не должна разбираться", s)
		}
	}
}

// ── SplitCommandLine: кавычки ──

func TestSplitCommandLine(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"connect сервер", []string{"connect", "сервер"}},
		{`connect "wdtt://1.2.3.4:1:2:3:pass#Мой сервер"`,
			[]string{"connect", "wdtt://1.2.3.4:1:2:3:pass#Мой сервер"}},
		{"  extra   spaces ", []string{"extra", "spaces"}},
		{`servers set-hashes имя h1,h2`, []string{"servers", "set-hashes", "имя", "h1,h2"}},
	}
	for _, c := range cases {
		got := cli.SplitCommandLine(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitCommandLine(%q) = %#v, ожидалось %#v", c.in, got, c.want)
		}
	}
}

// ── Строка из одних кавычек: пустой args, без паники (XW-04) ──

func TestSplitCommandLine_OnlyQuotes(t *testing.T) {
	for _, in := range []string{`""`, `"`, `"""`, `  ""  `} {
		if got := cli.SplitCommandLine(in); len(got) != 0 {
			t.Errorf("SplitCommandLine(%q) = %#v, ожидался пустой список", in, got)
		}
	}
}

// ── Ссылка с пробелами в кавычках (интерактивный ввод) ──

func TestSplitCommandLine_QuotesInside(t *testing.T) {
	got := cli.SplitCommandLine(`connect "name with spaces" --workers 18`)
	want := []string{"connect", "name with spaces", "--workers", "18"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, ожидалось %#v", got, want)
	}
}
