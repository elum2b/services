package repository

import (
	"testing"

	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func TestMapPaymentReportStatsUsesGlobalUniqueBuyerCount(t *testing.T) {
	stats := mapPaymentReportStats([]paymentsqlc.AdminGetPaymentReportStatsRow{
		{AssetCode: "RUB", OrderCount: 1, UniqueBuyers: 2},
		{AssetCode: "XTR", OrderCount: 3, UniqueBuyers: 2},
	})

	if stats.TotalOrders != 4 {
		t.Fatalf("total orders = %d, want 4", stats.TotalOrders)
	}

	if stats.UniqueBuyers != 2 {
		t.Fatalf("unique buyers = %d, want global count 2", stats.UniqueBuyers)
	}
}
