package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestFinanceBatchAtomicOwnershipAndRetry(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	first, second := financeClaimBody(t, student), financeClaimBody(t, student)
	items := []map[string]any{first, second}
	batch := func(want int) []financeTestClaim {
		raw := student.request("POST", "/api/finance/claims/batch", map[string]any{"requestId": "atomic", "items": items}, want)
		if want != 200 && want != 201 {
			return nil
		}
		return unmarshal[[]financeTestClaim](t, raw["items"])
	}
	count := func(table string) int {
		var n int
		if err := a.store.row("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	second["amountCents"] = -1
	batch(400)
	if count("finance_claims") != 0 || count("finance_files") != 0 {
		t.Fatal("invalid batch left partial records")
	}
	second["amountCents"] = 23456
	foreign := uploadForTest(t, admin, "foreign.pdf", []byte("%PDF-1.7\nadmin"), 201)
	ownSecond := second["invoiceFileIds"]
	second["invoiceFileIds"] = []string{foreign.ID}
	batch(403)
	if count("finance_claims") != 0 || count("finance_files") != 0 {
		t.Fatal("foreign receipt left partial records")
	}
	second["invoiceFileIds"] = first["invoiceFileIds"]
	batch(400)
	second["invoiceFileIds"] = ownSecond
	created := batch(201)
	if len(created) != 2 || count("finance_claims") != 2 {
		t.Fatal("batch not created together")
	}
	retried := batch(200)
	if len(retried) != 2 || retried[0].ID != created[0].ID || count("finance_claims") != 2 {
		t.Fatal("retry duplicated claims")
	}
	second["amountCents"] = 56789
	batch(409)
	if count("finance_claims") != 2 {
		t.Fatal("changed retry modified claims")
	}
	student.request("POST", "/api/finance/claims/batch", map[string]any{"requestId": "empty", "items": []any{}}, 400)
	student.request("POST", "/api/finance/claims/batch", map[string]any{"requestId": "large", "items": make([]FinanceClaimFields, 21)}, 400)
	student.request("POST", "/api/finance/claims/batch", map[string]any{"items": items}, 400)
	raw := admin.request("POST", "/api/finance/claims/batch", map[string]any{"requestId": "atomic", "items": []any{financeClaimBody(t, admin)}}, 201)
	other := unmarshal[[]financeTestClaim](t, raw["items"])
	if len(other) != 1 || other[0].UserID == created[0].UserID || count("finance_claims") != 3 {
		t.Fatal("idempotency must be scoped by owner")
	}
}

func TestFinanceSupplementInvoicesPreservesPaidClaim(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	stranger := &testClient{t: t, handler: admin.handler}
	stranger.login("mentor")
	body := financeClaimBody(t, student)
	body["invoiceFileIds"] = []string{}
	claim := financeCreateClaim(t, student, body)
	approved := financeAction(t, admin, claim, "approve", nil, 200)
	paid := financeAction(t, admin, approved, "pay", map[string]any{"paidOn": "2024-10-20", "paymentReference": "paid-before-invoice"}, 200)
	before, err := readOne[FinanceClaim](a.store, "SELECT payload FROM finance_claims WHERE id=?", paid.ID)
	if err != nil {
		t.Fatal(err)
	}
	ledgerBefore, _ := json.Marshal(financeLedger(t, admin))
	invoice := uploadForTest(t, student, "late-invoice.pdf", []byte("%PDF-1.7\nlate-invoice"), 201)
	extra := map[string]any{"invoiceFileIds": []string{invoice.ID}, "amountCents": 999999, "title": "malicious change", "paymentFileIds": []string{}, "paidOn": "2020-01-01", "paymentReference": "changed"}
	financeAction(t, admin, paid, "supplement", extra, 403)
	financeAction(t, stranger, paid, "supplement", extra, 403)
	financeAction(t, student, paid, "supplement", nil, 400)
	after := financeAction(t, student, paid, "supplement", extra, 200)
	if after.Status != "paid" || after.AmountCents != paid.AmountCents || after.Version != paid.Version+1 || len(after.InvoiceFileIDs) != 1 || len(after.PaymentFileIDs) != 1 || len(after.Revisions) != 1 || len(after.Revisions[0].InvoiceFileIDs) != 0 {
		t.Fatalf("invalid supplement: %+v", after)
	}
	stored, err := readOne[FinanceClaim](a.store, "SELECT payload FROM finance_claims WHERE id=?", paid.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title != before.Title || stored.PaidOn != before.PaidOn || stored.PaymentReference != before.PaymentReference || stored.ReviewNote != before.ReviewNote {
		t.Fatal("supplement changed approved data")
	}
	ledgerAfter, _ := json.Marshal(financeLedger(t, admin))
	if !bytes.Equal(ledgerBefore, ledgerAfter) {
		t.Fatal("supplement changed cash ledger")
	}
	financeAction(t, student, paid, "supplement", extra, 409)
	financeAction(t, student, after, "supplement", extra, 400)
	if stored.Events[len(stored.Events)-1].Action != "supplement" {
		t.Fatal("missing audit event")
	}
	downloadForTest(t, admin, invoice.ID, "download", 200)
	downloadForTest(t, stranger, invoice.ID, "download", 403)
	for _, status := range []string{"submitted", "approved", "returned", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			c := financeCreateClaim(t, student, body)
			if status == "approved" {
				c = financeAction(t, admin, c, "approve", nil, 200)
			}
			if status == "returned" {
				c = financeAction(t, admin, c, "return", map[string]any{"reason": "invoice needed"}, 200)
			}
			if status == "cancelled" {
				c = financeAction(t, student, c, "cancel", nil, 200)
			}
			expected := 200
			if status == "cancelled" {
				expected = 409
			}
			got := financeAction(t, student, c, "supplement", map[string]any{"invoiceFileIds": []string{invoice.ID}}, expected)
			if expected == 200 && got.Status != status {
				t.Fatal("supplement changed review status")
			}
		})
	}
}
