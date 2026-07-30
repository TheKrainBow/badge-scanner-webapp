package wiegand

import "testing"

func TestHeinzBadge(t *testing.T) {
	c, err := FromUID([]byte{0xE0, 0x1C, 0xBE, 0xDB})
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, "uidHex", c.UIDHex, "E01CBEDB")
	assertEq(t, "facilityCode", c.FacilityCode, 190)
	assertEq(t, "cardNumber", c.CardNumber, 7392)
	assertEq(t, "wiegand26", c.Wiegand26, "19007392")
}

func TestF204632A(t *testing.T) {
	c, err := FromUID([]byte{0xF2, 0x04, 0x63, 0x2A})
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, "mifareHex", c.MifareHex, "F204632A")
	assertEq(t, "facilityCode", c.FacilityCode, 99)
	assertEq(t, "cardNumber", c.CardNumber, 1266)
	assertEq(t, "wiegand26", c.Wiegand26, "9901266")
	assertEq(t, "wiegandUnpadded", c.WiegandUnpadded, "991266")
	assertEq(t, "premium", c.Premium, int64(711132402))
}

func Test04A2C81D(t *testing.T) {
	c, err := FromUID([]byte{0x04, 0xA2, 0xC8, 0x1D})
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, "mifareHex", c.MifareHex, "04A2C81D")
	assertEq(t, "facilityCode", c.FacilityCode, 200)
	assertEq(t, "cardNumber", c.CardNumber, 41476)
	assertEq(t, "wiegand26", c.Wiegand26, "20041476")
	assertEq(t, "wiegandUnpadded", c.WiegandUnpadded, "20041476")
	assertEq(t, "premium", c.Premium, int64(499687940))

	candidates := c.CACandidates()
	if len(candidates) != 2 || candidates[0] != "20041476" || candidates[1] != "499687940" {
		t.Fatalf("caCandidates = %v, want [20041476 499687940]", candidates)
	}
}

func TestABCD1ACE(t *testing.T) {
	c, err := FromUID([]byte{0xAB, 0xCD, 0x1A, 0xCE})
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, "mifareHex", c.MifareHex, "ABCD1ACE")
	assertEq(t, "facilityCode", c.FacilityCode, 26)
	assertEq(t, "cardNumber", c.CardNumber, 52651)
	assertEq(t, "wiegand26", c.Wiegand26, "2652651")
	assertEq(t, "wiegandUnpadded", c.WiegandUnpadded, "2652651")
	assertEq(t, "premium", c.Premium, int64(3457863083))
}

func TestUsesFirstFourBytesOfLongerUIDs(t *testing.T) {
	short, _ := FromUID([]byte{0xF2, 0x04, 0x63, 0x2A})
	long, _ := FromUID([]byte{0xF2, 0x04, 0x63, 0x2A, 0x11, 0x22, 0x33})
	assertEq(t, "wiegand26", short.Wiegand26, long.Wiegand26)
	assertEq(t, "premium", short.Premium, long.Premium)
}

func assertEq(t *testing.T, name string, got, want any) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}
