// Package wiegand ports mobile/app/.../nfc/Wiegand.kt: turns a badge UID
// into the candidate codes the CA stores as its user id.
package wiegand

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// Codes mirrors Kotlin's BadgeCodes.
type Codes struct {
	UIDHex          string
	MifareHex       string
	MifareDecimal   int64
	FacilityCode    int
	CardNumber      int
	Wiegand26       string
	WiegandUnpadded string
	Premium         int64
}

// CACandidates are the badge ids worth trying against the CA `/users/{id}`
// endpoint, most likely format first — matches BadgeCodes.caCandidates.
func (c Codes) CACandidates() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range []string{c.Wiegand26, c.WiegandUnpadded, fmt.Sprintf("%d", c.Premium)} {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// FromUIDHex rebuilds the codes from a stored UID hex string.
func FromUIDHex(uidHex string) (Codes, error) {
	b, err := HexToBytes(uidHex)
	if err != nil {
		return Codes{}, err
	}
	return FromUID(b)
}

// FromUID computes the Wiegand codes from raw UID bytes, exactly like
// Wiegand.fromUid: only the first 4 bytes matter (what Wiegand readers put
// on the bus); shorter UIDs are left-padded with zeros.
func FromUID(uid []byte) (Codes, error) {
	if len(uid) == 0 {
		return Codes{}, fmt.Errorf("empty tag UID")
	}
	var u []byte
	if len(uid) >= 4 {
		u = uid[0:4]
	} else {
		u = append(make([]byte, 4-len(uid)), uid...)
	}
	b0, b1, b2, b3 := int64(u[0]), int64(u[1]), int64(u[2]), int64(u[3])

	mifareDecimal := (b0 << 24) | (b1 << 16) | (b2 << 8) | b3
	premium := (b3 << 24) | (b2 << 16) | (b1 << 8) | b0
	facilityCode := int(b2)
	cardNumber := int((b1 << 8) | b0)

	return Codes{
		UIDHex:          strings.ToUpper(hex.EncodeToString(uid)),
		MifareHex:       fmt.Sprintf("%08X", mifareDecimal),
		MifareDecimal:   mifareDecimal,
		FacilityCode:    facilityCode,
		CardNumber:      cardNumber,
		Wiegand26:       fmt.Sprintf("%d%05d", facilityCode, cardNumber),
		WiegandUnpadded: fmt.Sprintf("%d%d", facilityCode, cardNumber),
		Premium:         premium,
	}, nil
}

// HexToBytes parses a hex string the same way String.hexToByteArray does.
func HexToBytes(s string) ([]byte, error) {
	clean := strings.TrimSpace(s)
	if len(clean)%2 != 0 {
		return nil, fmt.Errorf("odd-length hex string")
	}
	return hex.DecodeString(clean)
}
