//go:build windows

package main

import (
	"net/netip"
	"testing"
)

// TestDecideNAT — приведение состояния NAT к нужному виду.
func TestDecideNAT(t *testing.T) {
	const prefix = "10.77.1.0/24"

	tests := []struct {
		name  string
		cur   natInfo
		want  bool
		act   natAction
	}{
		{
			name: "нет объекта, галочка включена — создать",
			cur:  natInfo{},
			want: true,
			act:  natCreate,
		},
		{
			name: "объект есть и активен — ничего не делать",
			cur:  natInfo{Present: true, Active: true, Prefix: prefix},
			want: true,
			act:  natNothing,
		},
		{
			name: "другая подсеть — пересоздать",
			cur:  natInfo{Present: true, Active: true, Prefix: "10.77.0.0/16"},
			want: true,
			act:  natRecreate,
		},
		{
			name: "объект есть, но не активен — пересоздать",
			cur:  natInfo{Present: true, Active: false, Prefix: prefix},
			want: true,
			act:  natRecreate,
		},
		{
			name: "галочка выключена, объект есть — снять",
			cur:  natInfo{Present: true, Active: true, Prefix: prefix},
			want: false,
			act:  natRemove,
		},
		{
			name: "галочка выключена, объекта нет — ничего",
			cur:  natInfo{},
			want: false,
			act:  natNothing,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideNAT(tc.cur, prefix, tc.want); got != tc.act {
				t.Fatalf("decideNAT() = %v, ждали %v", got, tc.act)
			}
		})
	}
}

// TestParseNATStatus — разбор вывода Get-NetNat.
func TestParseNATStatus(t *testing.T) {
	info, err := parseNATStatus(`{"Name":"POSvpn","InternalIPInterfaceAddressPrefix":"10.77.1.0/24","Active":true}`)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if !info.Present || !info.Active || info.Prefix != "10.77.1.0/24" {
		t.Fatalf("состояние: %+v", info)
	}

	// PowerShell 5.1 сериализует Active как 1 — так и приходит с живой системы.
	info, err = parseNATStatus(`{"Name":"POSvpn","InternalIPInterfaceAddressPrefix":"10.77.0.0/16","Active":1}`)
	if err != nil {
		t.Fatalf("разбор Active=1: %v", err)
	}
	if !info.Active {
		t.Fatalf("Active=1 не распознан: %+v", info)
	}

	for _, empty := range []string{"", "  ", "null"} {
		info, err := parseNATStatus(empty)
		if err != nil {
			t.Fatalf("пустой вывод %q: %v", empty, err)
		}
		if info.Present {
			t.Fatalf("пустой вывод %q дал объект: %+v", empty, info)
		}
	}

	if _, err := parseNATStatus("{не json}"); err == nil {
		t.Fatal("ожидалась ошибка разбора")
	}
}

// TestValidateNATPrefix — какие подсети допустимы.
func TestValidateNATPrefix(t *testing.T) {
	cases := map[string]string{
		"10.77.1.0/24": "10.77.1.0/24",
		"10.77.1.5/24": "10.77.1.0/24", // адрес не в сети — маскируется
		"10.77.0.0/16": "10.77.0.0/16",
		"10.77.1.3/32": "10.77.1.3/32",
	}
	for in, want := range cases {
		prefix, err := netip.ParsePrefix(in)
		if err != nil {
			t.Fatal(err)
		}
		got, err := validateNATPrefix(prefix)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got != want {
			t.Fatalf("%s: получили %q, ждали %q", in, got, want)
		}
	}

	bad := []netip.Prefix{
		{},
		netip.MustParsePrefix("fd00::/64"),
		netip.MustParsePrefix("0.0.0.0/0"),
		netip.MustParsePrefix("10.0.0.0/7"),
	}
	for _, prefix := range bad {
		if _, err := validateNATPrefix(prefix); err == nil {
			t.Fatalf("%s должен быть отвергнут", prefix)
		}
	}
}

// TestAdapterNameRe — имя адаптера попадает в сценарий PowerShell, поэтому
// кавычки и спецсимволы запрещены, а кириллица разрешена.
func TestAdapterNameRe(t *testing.T) {
	good := []string{"POSvpn", "Подключение по локальной сети", "Ethernet 2", "vEthernet (Default Switch)", `a"b`}
	for _, name := range good {
		if !adapterNameRe.MatchString(name) {
			t.Fatalf("%q должен быть допустим", name)
		}
	}
	bad := []string{"", "POSvpn'; Remove-NetNat", "a\nb"}
	for _, name := range bad {
		if adapterNameRe.MatchString(name) {
			t.Fatalf("%q должен быть отвергнут", name)
		}
	}
}
