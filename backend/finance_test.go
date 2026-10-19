package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

type financeTestClaim struct {
	ID             string            `json:"id"`
	UserID         string            `json:"userId"`
	Status         string            `json:"status"`
	Version        int               `json:"version"`
	AmountCents    int64             `json:"amountCents"`
	InvoiceFileIDs []string          `json:"invoiceFileIds"`
	PaymentFileIDs []string          `json:"paymentFileIds"`
	PayoutFileIDs  []string          `json:"payoutFileIds"`
	Events         []json.RawMessage `json:"events"`
	Revisions      []struct {
		InvoiceFileIDs []string `json:"invoiceFileIds"`
		Version        int      `json:"version"`
	} `json:"revisions"`
}

type financeTestLedger struct {
	Items []struct {
		ID          string `json:"id"`
		ClaimID     string `json:"claimId"`
		Direction   string `json:"direction"`
		AmountCents int64  `json:"amountCents"`
	} `json:"items"`
	Summary struct {
		IncomeCents  int64 `json:"incomeCents"`
		ExpenseCents int64 `json:"expenseCents"`
		BalanceCents int64 `json:"balanceCents"`
		PendingCents int64 `json:"pendingCents"`
	} `json:"summary"`
}

func financeRequestRaw(c *testClient, method, path string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	return w
}

func financeClaimBody(t *testing.T, c *testClient) map[string]any {
	t.Helper()
	invoice := uploadForTest(t, c, "invoice.pdf", []byte("%PDF-1.7\nfinance-invoice"), 201)
	payment := uploadForTest(t, c, "payment.pdf", []byte("%PDF-1.7\nfinance-payment"), 201)
	return map[string]any{"title": "机器人器材报销", "amountCents": 12345, "currency": "CNY", "category": "器材采购", "incurredOn": "2024-10-19", "notes": "用于测试的凭证", "invoiceFileIds": []string{invoice.ID}, "paymentFileIds": []string{payment.ID}}
}

func financeCreateClaim(t *testing.T, c *testClient, body map[string]any) financeTestClaim {
	t.Helper()
	return unmarshal[financeTestClaim](t, c.request("POST", "/api/finance/claims", body, 201)["item"])
}

func financeAction(t *testing.T, c *testClient, claim financeTestClaim, action string, extra map[string]any, expected int) financeTestClaim {
	t.Helper()
	body := map[string]any{"action": action, "version": claim.Version}
	for k, v := range extra {
		body[k] = v
	}
	raw := c.request("PATCH", "/api/finance/claims/"+claim.ID, body, expected)
	if expected != 200 {
		return financeTestClaim{}
	}
	return unmarshal[financeTestClaim](t, raw["item"])
}

func financeLedger(t *testing.T, c *testClient) financeTestLedger {
	t.Helper()
	w := financeRequestRaw(c, "GET", "/api/finance/ledger", nil)
	if w.Code != 200 {
		t.Fatalf("ledger: %d %s", w.Code, w.Body.String())
	}
	return unmarshal[financeTestLedger](t, w.Body.Bytes())
}

func TestFinanceClaimReviewPaymentAndPrivacy(t *testing.T) {
	_, admin := setup(t)
	admin.login("admin")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	mentor := &testClient{t: t, handler: admin.handler}
	mentor.login("mentor")
	stranger := &testClient{t: t, handler: admin.handler}
	stranger.login("interviewer")
	body := financeClaimBody(t, student)
	claim := financeCreateClaim(t, student, body)
	if claim.Status != "submitted" || claim.Version < 1 || claim.UserID != "demo-student" {
		t.Fatalf("invalid initial claim: %+v", claim)
	}
	for _, c := range []*testClient{mentor, stranger} {
		claims := unmarshal[[]financeTestClaim](t, c.request("GET", "/api/finance/claims", nil, 200)["items"])
		for _, item := range claims {
			if item.ID == claim.ID {
				t.Fatal("another user sees private claim")
			}
		}
		c.request("GET", "/api/finance/ledger", nil, 403)
		financeAction(t, c, claim, "approve", nil, 403)
		downloadForTest(t, c, claim.InvoiceFileIDs[0], "download", 403)
	}
	downloadForTest(t, student, claim.InvoiceFileIDs[0], "download", 200)
	downloadForTest(t, admin, claim.PaymentFileIDs[0], "download", 200)
	financeAction(t, student, claim, "approve", nil, 403)
	adminMine := unmarshal[[]financeTestClaim](t, admin.request("GET", "/api/finance/claims?scope=mine", nil, 200)["items"])
	if len(adminMine) != 0 {
		t.Fatal("admin personal view includes another user claim")
	}
	approved := financeAction(t, admin, claim, "approve", nil, 200)
	if approved.Status != "approved" || approved.Version <= claim.Version {
		t.Fatal("approval state/version incorrect")
	}
	ledger := financeLedger(t, admin)
	if len(ledger.Items) != 0 || ledger.Summary.PendingCents != claim.AmountCents || ledger.Summary.BalanceCents != 0 {
		t.Fatalf("approval must not record payment: %+v", ledger)
	}
	financeAction(t, admin, claim, "pay", map[string]any{"paidOn": "2024-10-20"}, 409)
	payout := uploadForTest(t, admin, "payout.pdf", []byte("%PDF-1.7\npayout-private"), 201)
	paid := financeAction(t, admin, approved, "pay", map[string]any{"paidOn": "2024-10-20", "paymentReference": "bank-test-001", "payoutFileIds": []string{payout.ID}}, 200)
	financeAction(t, student, paid, "cancel", nil, 409)
	if paid.Status != "paid" {
		t.Fatalf("payment not completed: %+v", paid)
	}
	financeAction(t, admin, approved, "pay", map[string]any{"paidOn": "2024-10-20"}, 409)
	financeAction(t, admin, paid, "pay", map[string]any{"paidOn": "2024-10-20"}, 409)
	ledger = financeLedger(t, admin)
	if len(ledger.Items) != 1 || ledger.Items[0].ClaimID != claim.ID || ledger.Summary.ExpenseCents != 12345 || ledger.Summary.BalanceCents != -12345 || ledger.Summary.PendingCents != 0 {
		t.Fatalf("payment ledger mismatch: %+v", ledger)
	}
	admin.request("POST", "/api/finance/ledger/"+ledger.Items[0].ID+"/reverse", map[string]string{"reason": "不能冲销报销付款"}, 409)
	downloadForTest(t, student, payout.ID, "download", 200)
	downloadForTest(t, mentor, payout.ID, "download", 403)
	downloadForTest(t, stranger, payout.ID, "download", 403)
}

func TestFinanceReturnedResubmissionAndCancellation(t *testing.T) {
	_, admin := setup(t)
	admin.login("admin")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	body := financeClaimBody(t, student)
	claim := financeCreateClaim(t, student, body)
	financeAction(t, admin, claim, "return", map[string]any{"reason": "  "}, 400)
	returned := financeAction(t, admin, claim, "return", map[string]any{"reason": "请补充完整发票"}, 200)
	if returned.Status != "returned" {
		t.Fatalf("return state: %+v", returned)
	}
	beforeEvents := len(returned.Events)
	if beforeEvents != 2 || !bytes.Contains(returned.Events[1], []byte("请补充完整发票")) {
		t.Fatal("return reason not preserved in audit events")
	}
	oldInvoice := claim.InvoiceFileIDs[0]
	newInvoice := uploadForTest(t, student, "replacement.pdf", []byte("%PDF-1.7\nreplacement-invoice"), 201)
	body["invoiceFileIds"] = []string{newInvoice.ID}
	body["notes"] = "已补充完整票据"
	changed := financeAction(t, student, returned, "resubmit", body, 200)
	if changed.Status != "submitted" || len(changed.Events) <= beforeEvents || changed.InvoiceFileIDs[0] != newInvoice.ID {
		t.Fatalf("resubmission/history mismatch: %+v", changed)
	}
	if len(changed.Revisions) != 1 || changed.Revisions[0].InvoiceFileIDs[0] != oldInvoice || changed.Revisions[0].Version != returned.Version {
		t.Fatal("resubmit did not preserve prior receipt snapshot")
	}
	downloadForTest(t, admin, oldInvoice, "download", 200)
	downloadForTest(t, student, oldInvoice, "download", 200)
	// A financial reviewer cannot edit the claimant's amount or receipts.
	financeAction(t, admin, changed, "resubmit", body, 403)
	cancelled := financeAction(t, student, changed, "cancel", nil, 200)
	if cancelled.Status != "cancelled" {
		t.Fatal("claim not cancelled")
	}
	financeAction(t, admin, cancelled, "approve", nil, 409)
	if len(financeLedger(t, admin).Items) != 0 {
		t.Fatal("returned or cancelled claim entered ledger")
	}
}

func TestFinanceClaimValidationAndFileOwnership(t *testing.T) {
	_, admin := setup(t)
	admin.login("admin")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	base := financeClaimBody(t, student)
	cases := []struct {
		name, key string
		value     any
	}{
		{"zero", "amountCents", 0}, {"negative", "amountCents", -1}, {"fraction", "amountCents", 12.5}, {"overflow", "amountCents", 1e30}, {"above-limit", "amountCents", int64(100000000001)},
		{"future-date", "incurredOn", "2099-12-31"}, {"invalid-date", "incurredOn", "2026-02-30"}, {"missing-date", "incurredOn", ""}, {"invalid-currency", "currency", "USD"},
		{"unknown-file", "invoiceFileIds", []string{"does-not-exist"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{}
			for k, v := range base {
				body[k] = v
			}
			body[tc.key] = tc.value
			w := financeRequestRaw(student, "POST", "/api/finance/claims", body)
			if w.Code < 400 || w.Code >= 500 {
				t.Fatalf("invalid input accepted: %d %s", w.Code, w.Body.String())
			}
		})
	}
	empty := map[string]any{}
	for k, v := range base {
		empty[k] = v
	}
	empty["invoiceFileIds"] = []string{}
	empty["paymentFileIds"] = []string{}
	student.request("POST", "/api/finance/claims", empty, 400)
	foreignFile := uploadForTest(t, admin, "private-admin.pdf", []byte("%PDF-1.7\nprivate"), 201)
	base["invoiceFileIds"] = []string{foreignFile.ID}
	w := financeRequestRaw(student, "POST", "/api/finance/claims", base)
	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("other owner's receipt accepted: %d %s", w.Code, w.Body.String())
	}
}

func TestFinanceManualLedgerReversal(t *testing.T) {
	_, admin := setup(t)
	admin.login("admin")
	body := map[string]any{"direction": "income", "amountCents": 50000, "occurredOn": "2024-10-19", "category": "经费拨款", "counterparty": "学院", "note": "期初资金", "fileIds": []string{}}
	income := unmarshal[map[string]any](t, admin.request("POST", "/api/finance/ledger", body, 201)["item"])
	body["direction"] = "expense"
	body["amountCents"] = 1200
	body["category"] = "采购"
	body["note"] = "直接支付"
	expense := unmarshal[map[string]any](t, admin.request("POST", "/api/finance/ledger", body, 201)["item"])
	ledger := financeLedger(t, admin)
	if ledger.Summary.IncomeCents != 50000 || ledger.Summary.ExpenseCents != 1200 || ledger.Summary.BalanceCents != 48800 {
		t.Fatalf("manual ledger balance: %+v", ledger)
	}
	admin.request("POST", "/api/finance/ledger/"+expense["id"].(string)+"/reverse", map[string]string{"reason": ""}, 400)
	admin.request("POST", "/api/finance/ledger/"+expense["id"].(string)+"/reverse", map[string]string{"reason": "录入错误"}, 200)
	admin.request("POST", "/api/finance/ledger/"+expense["id"].(string)+"/reverse", map[string]string{"reason": "重复冲销"}, 409)
	ledger = financeLedger(t, admin)
	if ledger.Summary.BalanceCents != 50000 || ledger.Summary.ExpenseCents != 0 || ledger.Summary.IncomeCents != 50000 {
		t.Fatalf("reversal did not restore balance: %+v", ledger)
	}
	retained := false
	for _, item := range ledger.Items {
		if item.ID == expense["id"] {
			retained = true
		}
	}
	if !retained {
		t.Fatal("reversal deleted original ledger entry")
	}
	if income["id"] == expense["id"] {
		t.Fatal("distinct ledger entries share ID")
	}
}

func TestFinanceExportsSelectedReceiptsAndCSVSafeCells(t *testing.T) {
	_, admin := setup(t)
	admin.login("admin")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	body := financeClaimBody(t, student)
	body["title"] = "=HYPERLINK(\"https://example.test\")"
	body["notes"] = "+危险公式"
	selected := financeCreateClaim(t, student, body)
	otherBody := financeClaimBody(t, student)
	otherProof := uploadForTest(t, student, "unselected.pdf", []byte("%PDF-1.7\nunselected-private-invoice"), 201)
	otherBody["invoiceFileIds"] = []string{otherProof.ID}
	other := financeCreateClaim(t, student, otherBody)
	query := "/api/finance/export?ids=" + selected.ID
	csvOut := financeRequestRaw(admin, "GET", query+"&format=csv", nil)
	if csvOut.Code != 200 {
		t.Fatalf("CSV export: %d %s", csvOut.Code, csvOut.Body.String())
	}
	data := csvOut.Body.Bytes()
	if !bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		t.Fatal("CSV requires UTF-8 BOM for spreadsheet Chinese text")
	}
	rows, err := csv.NewReader(strings.NewReader(string(data[3:]))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("selected export should have header and one claim: %d", len(rows))
	}
	foundTitle := false
	for _, cell := range rows[1] {
		if strings.Contains(cell, "HYPERLINK") {
			foundTitle = true
			if !strings.HasPrefix(cell, "'") {
				t.Fatalf("unescaped formula cell %q", cell)
			}
		}
	}
	if !foundTitle {
		t.Fatal("CSV did not include claim title")
	}
	zipOut := financeRequestRaw(admin, "GET", query+"&format=zip", nil)
	if zipOut.Code != 200 {
		t.Fatalf("ZIP export: %d %s", zipOut.Code, zipOut.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(zipOut.Body.Bytes()), int64(zipOut.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	invoice, payment := false, false
	for _, file := range zr.File {
		if strings.HasPrefix(file.Name, "/") || strings.Contains(file.Name, "../") || strings.Contains(file.Name, other.ID) {
			t.Fatalf("unsafe or out-of-selection archive entry %q", file.Name)
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(contents, []byte("unselected-private-invoice")) {
			t.Fatal("unselected private receipt included in ZIP")
		}
		if strings.Contains(string(contents), "finance-invoice") {
			invoice = true
		}
		if strings.Contains(string(contents), "finance-payment") {
			payment = true
		}
	}
	if !invoice || !payment {
		t.Fatal("ZIP missing selected invoice/payment proof")
	}
	student.request("GET", query+"&format=csv", nil, 403)
	ledgerCSV := financeRequestRaw(admin, "GET", "/api/finance/ledger/export", nil)
	if ledgerCSV.Code != 200 || !bytes.HasPrefix(ledgerCSV.Body.Bytes(), []byte{0xef, 0xbb, 0xbf}) {
		t.Fatalf("ledger CSV export: %d", ledgerCSV.Code)
	}
}

func exerciseFinancePaymentRace(t *testing.T, a, other *App) {
	t.Helper()
	admin := &testClient{t: t, handler: a.Handler()}
	admin.login("admin")
	secondAdmin := &testClient{t: t, handler: other.Handler()}
	secondAdmin.login("admin")
	student := &testClient{t: t, handler: a.Handler()}
	student.login("student")
	claim := financeCreateClaim(t, student, financeClaimBody(t, student))
	approved := financeAction(t, admin, claim, "approve", nil, 200)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, c := range []*testClient{admin, secondAdmin} {
		go func(c *testClient) {
			<-start
			results <- financeRequestRaw(c, "PATCH", "/api/finance/claims/"+claim.ID, map[string]any{"action": "pay", "version": approved.Version, "paidOn": "2024-10-20", "paymentReference": "concurrent-transfer"})
		}(c)
	}
	close(start)
	counts := map[int]int{}
	for i := 0; i < 2; i++ {
		w := <-results
		counts[w.Code]++
		if w.Code != 200 && w.Code != 409 {
			t.Errorf("concurrent pay: %d %s", w.Code, w.Body.String())
		}
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("payment must commit exactly once: %+v", counts)
	}
	ledger := financeLedger(t, admin)
	count := 0
	for _, item := range ledger.Items {
		if item.ClaimID == claim.ID {
			count++
			if item.AmountCents != claim.AmountCents {
				t.Fatal("race ledger amount changed")
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one ledger expense, got %d", count)
	}
}

func TestFinanceConcurrentPayment(t *testing.T) {
	a, _ := setup(t)
	exerciseFinancePaymentRace(t, a, a)
}

func exerciseFinancePostgres(t *testing.T, databaseURL string, a *App) {
	t.Helper()
	s, err := openStore(databaseURL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	other := newApp(s, Config{Demo: true, WebDir: t.TempDir(), Mailer: &testMailer{}})
	exerciseFinancePaymentRace(t, a, other)
}

func TestFinanceReceiptsCannotEscapeThroughGenericFileFeatures(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	mentor := &testClient{t: t, handler: admin.handler}
	mentor.login("mentor")
	// A legacy public logo can later become a financial receipt. Its public route must close.
	imageData, err := base64.StdEncoding.DecodeString(testPhoto().ContentBase64)
	if err != nil {
		t.Fatal(err)
	}
	logo := uploadForTest(t, student, "previous-logo.png", imageData, 201)
	settings, err := a.store.settings()
	if err != nil {
		t.Fatal(err)
	}
	settings.LogoURL = "/api/public/logo/" + logo.ID
	if err = a.store.saveSettings(settings); err != nil {
		t.Fatal(err)
	}
	anonymous := &testClient{t: t, handler: admin.handler}
	before := financeRequestRaw(anonymous, "GET", settings.LogoURL, nil)
	if before.Code != 200 {
		t.Fatalf("legacy public logo fixture not accessible: %d", before.Code)
	}
	// OFD invoices need to upload and work as financial documents without image conversion.
	ofd := uploadForTest(t, student, "电子发票.ofd", []byte("PK\x03\x04test-ofd-invoice"), 201)
	body := financeClaimBody(t, student)
	body["invoiceFileIds"] = []string{ofd.ID, logo.ID}
	claim := financeCreateClaim(t, student, body)
	if len(claim.InvoiceFileIDs) != 2 {
		t.Fatal("OFD invoice omitted from claim")
	}
	anonymous.request("GET", settings.LogoURL, nil, 404)
	for _, id := range []string{"demo-student", "demo-admin"} {
		user, err := a.store.user(id)
		if err != nil {
			t.Fatal(err)
		}
		err = requireOwnedFiles(a.store, user, []string{ofd.ID})
		apiErr, ok := err.(*apiError)
		if !ok || apiErr.status != 400 {
			t.Fatalf("finance receipt reusable outside finance by %s: %v", id, err)
		}
	}
	if err = a.store.exec("INSERT INTO file_grants(file_id,user_id) VALUES(?,?)", ofd.ID, "demo-mentor"); err != nil {
		t.Fatal(err)
	}
	downloadForTest(t, mentor, ofd.ID, "download", 403)
	downloadForTest(t, student, ofd.ID, "download", 200)
	// Legacy assignment references must not bypass the file privacy check inside ZIP export.
	form := taskForm()
	form.Kind = "document"
	form.Category = "assignment"
	form.Judge = nil
	form.Questions = []TaskQuestion{{ID: "private-receipt", Title: "旧附件", Description: "历史引用测试", Type: "short_answer", MaxScore: 100}}
	task := taskForTest(t, admin, form)
	sub := Submission{ID: randomID(), TaskID: task.ID, UserID: "demo-student", UserName: "虚构学生", Version: 1, Status: "pending", FinalizedAt: now(), Answers: []SubmissionAnswer{{QuestionID: "private-receipt", FileIDs: []string{ofd.ID}}}}
	if err = a.store.exec("INSERT INTO submissions(id,task_id,user_id,version,payload) VALUES(?,?,?,?,?)", sub.ID, sub.TaskID, sub.UserID, sub.Version, encoded(sub)); err != nil {
		t.Fatal(err)
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{Direction: task.Direction, Permissions: []string{"tasks"}}})
	mentor.login("mentor")
	mentor.request("GET", "/api/tasks/"+task.ID+"/submissions/export", nil, 403)
}
