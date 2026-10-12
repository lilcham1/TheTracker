package core

import (
	"os"
	"strings"
	"testing"
)

func TestIniRoundTripAndEdit(t *testing.T) {
	src := "[/Script/Engine.GameUserSettings]\r\nbUseDesiredScreenHeight=False\r\n\r\n[ScalabilityGroups]\r\nsg.ShadowQuality=2\r\nsg.TextureQuality=1\r\n\r\n[/Script/TslGame.TslGameUserSettings]\r\nFpsCameraFov=103.000000\r\nOutgameUserDatas=(Key=\"X\",Value=\"secret=stays\")\r\nOutgameUserDatas=(Key=\"Y\",Value=\"\")\r\n"
	d := iniParse(src)
	if d.String() != src {
		t.Fatalf("round trip:\n%q\n%q", d.String(), src)
	}
	if v, ok := d.Get("ScalabilityGroups", "sg.ShadowQuality"); !ok || v != "2" {
		t.Fatalf("get: %q %v", v, ok)
	}
	if v, _ := d.Get("/Script/TslGame.TslGameUserSettings", "OutgameUserDatas"); v != `(Key="X",Value="secret=stays")` {
		t.Fatalf("value with = in it: %q", v)
	}
	d.Set("0", "ScalabilityGroups", "sg.ShadowQuality")
	d.Set("1", "ScalabilityGroups", "sg.EffectsQuality")
	d.Set("95.000000", "/Script/TslGame.TslGameUserSettings", "FpsCameraFov")
	d.Set("x", "NewSection", "Key")
	out := d.String()
	for _, want := range []string{"sg.ShadowQuality=0\r\nsg.TextureQuality=1\r\nsg.EffectsQuality=1\r\n\r\n[/Script/TslGame", "FpsCameraFov=95.000000\r\n", "OutgameUserDatas=(Key=\"X\",Value=\"secret=stays\")\r\nOutgameUserDatas=(Key=\"Y\"", "\r\n\r\n[NewSection]\r\nKey=x\r\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if !d.Delete("ScalabilityGroups", "sg.EffectsQuality") || strings.Contains(d.String(), "EffectsQuality") {
		t.Fatal("delete")
	}
	if iniParse("a=1").String() != "a=1" {
		t.Fatal("no trailing newline")
	}
}

// With TT_INI_FILE pointing at a real settings file, it must come back byte
// for byte. Read only, and nothing of it is printed.
func TestIniRealFileRoundTrip(t *testing.T) {
	path := os.Getenv("TT_INI_FILE")
	if path == "" {
		t.Skip()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if iniParse(string(raw)).String() != string(raw) {
		t.Fatal("round trip differs")
	}
	t.Logf("identical (%d bytes)", len(raw))
}
