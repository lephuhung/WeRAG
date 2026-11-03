package types

// ValidBankBIN reports whether bin is a bank BIN VietQR accepts: the
// 6-digit NAPAS code of the receiving bank (e.g. 970418 BIDV, 970436
// Vietcombank). Anything else makes img.vietqr.io return a broken image.
func ValidBankBIN(bin string) bool {
	if len(bin) != 6 {
		return false
	}
	for _, r := range bin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
