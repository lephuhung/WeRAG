package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// billing.bank_bin must be a 6-digit NAPAS BIN (or empty): anything else is
// refused on save, and a bad stored value never yields a VietQR image.
func TestBankBINValidation(t *testing.T) {
	for _, ok := range []string{"", "970418", "970436", " 970415 "} {
		require.NoError(t, validateRegistryEntry(types.BillingBankBINSettingKey, ok), ok)
	}
	for _, bad := range []string{"BIDV", "97041", "9704180", "970-18", "vcb970"} {
		err := validateRegistryEntry(types.BillingBankBINSettingKey, bad)
		require.Error(t, err, bad)
		require.Contains(t, err.Error(), "6-digit")
	}

	info := &types.UsageBillingInfo{Enabled: true, BankBIN: "970418", BankAccountNo: "123"}
	require.Contains(t, vietQRURL(info, 1000, "WRX"), "/970418-123-")
	for _, bad := range []string{"", "BIDV", "97041"} {
		info.BankBIN = bad
		require.Empty(t, vietQRURL(info, 1000, "WRX"), bad)
	}
}
