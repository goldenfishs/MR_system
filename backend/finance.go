package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

type FinanceEvent struct {
	Action    string `json:"action"`
	ActorName string `json:"actorName"`
	ActorID   string `json:"actorId"`
	At        string `json:"at"`
	Note      string `json:"note"`
}
type FinanceClaimFields struct {
	Title          string   `json:"title"`
	AmountCents    int64    `json:"amountCents"`
	Currency       string   `json:"currency"`
	Category       string   `json:"category"`
	IncurredOn     string   `json:"incurredOn"`
	Notes          string   `json:"notes"`
	InvoiceFileIDs []string `json:"invoiceFileIds"`
	PaymentFileIDs []string `json:"paymentFileIds"`
}
type FinanceClaim struct {
	FinanceClaimFields
	ID               string                 `json:"id"`
	UserID           string                 `json:"userId"`
	UserName         string                 `json:"userName"`
	Status           string                 `json:"status"`
	Version          int                    `json:"version"`
	CreatedAt        string                 `json:"createdAt"`
	UpdatedAt        string                 `json:"updatedAt"`
	ReviewNote       string                 `json:"reviewNote,omitempty"`
	PaidAt           string                 `json:"paidAt,omitempty"`
	PaidOn           string                 `json:"paidOn,omitempty"`
	PaymentReference string                 `json:"paymentReference,omitempty"`
	PayoutFileIDs    []string               `json:"payoutFileIds"`
	Events           []FinanceEvent         `json:"events"`
	Revisions        []FinanceClaimRevision `json:"revisions,omitempty"`
}
type FinanceClaimRevision struct {
	FinanceClaimFields
	Version int    `json:"version"`
	SavedAt string `json:"savedAt"`
}
type FinanceLedgerFields struct {
	Direction    string   `json:"direction"`
	AmountCents  int64    `json:"amountCents"`
	OccurredOn   string   `json:"occurredOn"`
	Category     string   `json:"category"`
	Counterparty string   `json:"counterparty"`
	Note         string   `json:"note"`
	FileIDs      []string `json:"fileIds"`
}
type FinanceLedgerEntry struct {
	FinanceLedgerFields
	ID             string `json:"id"`
	ClaimID        string `json:"claimId,omitempty"`
	CreatedAt      string `json:"createdAt"`
	CreatedByName  string `json:"createdByName"`
	CreatedBy      string `json:"createdBy"`
	ReversedAt     string `json:"reversedAt,omitempty"`
	ReversalReason string `json:"reversalReason,omitempty"`
	ReversedBy     string `json:"reversedBy,omitempty"`
}
type FinanceSummary struct {
	IncomeCents  int64 `json:"incomeCents"`
	ExpenseCents int64 `json:"expenseCents"`
	BalanceCents int64 `json:"balanceCents"`
	PendingCents int64 `json:"pendingCents"`
}

func (a *App) registerFinanceRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/finance/claims", a.wrap(true, a.listFinanceClaims))
	m.HandleFunc("POST /api/finance/claims", a.wrap(true, a.createFinanceClaim))
	m.HandleFunc("PATCH /api/finance/claims/{id}", a.wrap(true, a.updateFinanceClaim))
	m.HandleFunc("GET /api/finance/ledger", a.wrap(true, a.listFinanceLedger))
	m.HandleFunc("POST /api/finance/ledger", a.wrap(true, a.createFinanceLedger))
	m.HandleFunc("POST /api/finance/ledger/{id}/reverse", a.wrap(true, a.reverseFinanceLedger))
	m.HandleFunc("GET /api/finance/export", a.wrap(true, a.exportFinanceClaims))
	m.HandleFunc("GET /api/finance/ledger/export", a.wrap(true, a.exportFinanceLedger))
}
func financeAdmin(u User) error {
	if u.Role != "admin" {
		return fail(403, "只有管理员可管理财务")
	}
	return nil
}
func financeDate(v string) bool {
	t, err := time.Parse("2006-01-02", v)
	return err == nil && len(v) == 10 && t.Year() >= 2000 && t.Year() <= 2100
}
func financePastDate(v string) bool {
	return financeDate(v) && v <= time.Now().UTC().Add(8*time.Hour).Format("2006-01-02")
}
func financeAmount(v int64) bool { return v > 0 && v <= 100000000000 }
func financeText(v string, max int, required bool) bool {
	return len(v) <= max && (!required || strings.TrimSpace(v) != "") && !strings.ContainsRune(v, 0)
}
func (a *App) beginFinanceWrite(r *http.Request) (*App, *sql.Tx, error) {
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, nil, err
	}
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx, fileDir: a.store.fileDir}
	// Serialize state changes across independent server processes before reading balances/status.
	if err = s.exec("UPDATE finance_lock SET revision=revision WHERE id=1"); err != nil {
		tx.Rollback()
		return nil, nil, err
	}
	return &App{store: s, config: a.config}, tx, nil
}
func (a *App) financeFiles(u User, ids []string, recipient string) error {
	if len(ids) > 20 {
		return fail(400, "每次最多上传 20 份财务凭证")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return fail(400, "财务凭证不能重复")
		}
		seen[id] = true
		f, err := a.fileAsset(id)
		if err == sql.ErrNoRows {
			return fail(400, "财务凭证不存在")
		}
		if err != nil {
			return err
		}
		if f.OwnerID != u.ID {
			return fail(403, "财务凭证必须由当前用户上传")
		}
		if err = a.store.exec("INSERT INTO finance_files(file_id,user_id) VALUES(?,?) ON CONFLICT(file_id,user_id) DO NOTHING", id, recipient); err != nil {
			return err
		}
	}
	return nil
}
func (a *App) validateFinanceClaim(f *FinanceClaimFields, u User) error {
	f.Title = strings.TrimSpace(f.Title)
	f.Category = strings.TrimSpace(f.Category)
	f.Notes = strings.TrimSpace(f.Notes)
	if !financeText(f.Title, 240, true) || !financeText(f.Category, 120, true) || !financeText(f.Notes, 12000, false) || !financePastDate(f.IncurredOn) || !financeAmount(f.AmountCents) {
		return fail(400, "请填写有效的标题、类别、已发生日期及金额（最多 10 亿元）")
	}
	if f.Currency == "" {
		f.Currency = "CNY"
	}
	if f.Currency != "CNY" {
		return fail(400, "目前仅支持人民币")
	}
	if f.InvoiceFileIDs == nil {
		f.InvoiceFileIDs = []string{}
	}
	if f.PaymentFileIDs == nil {
		f.PaymentFileIDs = []string{}
	}
	if len(f.InvoiceFileIDs)+len(f.PaymentFileIDs) == 0 {
		return fail(400, "请至少上传一份发票或支付凭证")
	}
	return a.financeFiles(u, append(append([]string{}, f.InvoiceFileIDs...), f.PaymentFileIDs...), u.ID)
}
func (a *App) saveFinanceClaim(c FinanceClaim) error {
	return a.store.exec("INSERT INTO finance_claims(id,user_id,payload) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", c.ID, c.UserID, encoded(c))
}
func (c *FinanceClaim) event(action string, u User, note string) {
	c.Events = append(c.Events, FinanceEvent{Action: action, ActorID: u.ID, ActorName: u.Name, At: now(), Note: note})
	c.UpdatedAt = now()
}
func (a *App) createFinanceClaim(w http.ResponseWriter, r *http.Request, u User) error {
	var f FinanceClaimFields
	if err := decode(r, &f); err != nil {
		return err
	}
	scoped, tx, err := a.beginFinanceWrite(r)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a = scoped
	if err = a.validateFinanceClaim(&f, u); err != nil {
		return err
	}
	c := FinanceClaim{FinanceClaimFields: f, ID: randomID(), UserID: u.ID, UserName: u.Name, Status: "submitted", Version: 1, CreatedAt: now(), PayoutFileIDs: []string{}, Events: []FinanceEvent{}}
	c.event("submit", u, "")
	if err = a.saveFinanceClaim(c); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, 201, map[string]any{"item": c})
	return nil
}
func (a *App) updateFinanceClaim(w http.ResponseWriter, r *http.Request, u User) error {
	var in struct {
		FinanceClaimFields
		Action           string   `json:"action"`
		Version          int      `json:"version"`
		ReviewNote       string   `json:"reviewNote"`
		Note             string   `json:"note"`
		Reason           string   `json:"reason"`
		PaidOn           string   `json:"paidOn"`
		PaymentReference string   `json:"paymentReference"`
		PayoutFileIDs    []string `json:"payoutFileIds"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	scoped, tx, err := a.beginFinanceWrite(r)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a = scoped
	c, err := readOne[FinanceClaim](a.store, "SELECT payload FROM finance_claims WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	if u.Role != "admin" && c.UserID != u.ID {
		return fail(403, "无权修改此报销申请")
	}
	if c.Version != in.Version {
		return fail(409, "记录已更新，请刷新后重试")
	}
	if c.Status == "paid" || c.Status == "cancelled" {
		return fail(409, "已完成或已撤销的申请不可修改")
	}
	note := strings.TrimSpace(in.ReviewNote)
	if note == "" {
		note = strings.TrimSpace(in.Note)
	}
	if note == "" {
		note = strings.TrimSpace(in.Reason)
	}
	if !financeText(note, 4000, false) {
		return fail(400, "说明过长")
	}
	switch in.Action {
	case "approve":
		if err = financeAdmin(u); err != nil {
			return err
		}
		if c.Status != "submitted" {
			return fail(409, "只有待审核申请可以通过")
		}
		c.Status = "approved"
		c.ReviewNote = note
	case "return":
		if err = financeAdmin(u); err != nil {
			return err
		}
		if c.Status != "submitted" && c.Status != "approved" {
			return fail(409, "当前状态不能退回")
		}
		if note == "" {
			return fail(400, "请填写退回原因")
		}
		c.Status = "returned"
		c.ReviewNote = note
	case "resubmit":
		if c.UserID != u.ID {
			return fail(403, "只能由申请人重新提交")
		}
		if c.Status != "returned" {
			return fail(409, "只有退回的申请可以重新提交")
		}
		if err = a.validateFinanceClaim(&in.FinanceClaimFields, u); err != nil {
			return err
		}
		c.Revisions = append(c.Revisions, FinanceClaimRevision{FinanceClaimFields: c.FinanceClaimFields, Version: c.Version, SavedAt: now()})
		c.FinanceClaimFields = in.FinanceClaimFields
		c.Status = "submitted"
		c.ReviewNote = ""
	case "cancel":
		if c.UserID != u.ID {
			return fail(403, "只能由申请人撤销")
		}
		if c.Status != "submitted" && c.Status != "returned" {
			return fail(409, "审核通过后不能自行撤销")
		}
		c.Status = "cancelled"
	case "pay":
		if err = financeAdmin(u); err != nil {
			return err
		}
		if c.Status != "approved" {
			return fail(409, "只有已审核通过的申请可登记打款")
		}
		in.PaymentReference = strings.TrimSpace(in.PaymentReference)
		if !financePastDate(in.PaidOn) || in.PaidOn < c.IncurredOn || !financeText(in.PaymentReference, 240, true) {
			return fail(400, "请填写不早于费用日期的实际打款日期及打款流水号或说明")
		}
		if err = a.financeFiles(u, in.PayoutFileIDs, c.UserID); err != nil {
			return err
		}
		c.Status = "paid"
		c.PaidAt = now()
		c.PaidOn = in.PaidOn
		c.PaymentReference = in.PaymentReference
		c.PayoutFileIDs = in.PayoutFileIDs
		if c.PayoutFileIDs == nil {
			c.PayoutFileIDs = []string{}
		}
		entry := FinanceLedgerEntry{ID: randomID(), ClaimID: c.ID, CreatedAt: now(), CreatedBy: u.ID, CreatedByName: u.Name, FinanceLedgerFields: FinanceLedgerFields{Direction: "expense", AmountCents: c.AmountCents, OccurredOn: in.PaidOn, Category: c.Category, Counterparty: c.UserName, Note: c.Title + "；打款凭据：" + in.PaymentReference, FileIDs: c.PayoutFileIDs}}
		if err = a.saveFinanceLedger(entry); err != nil {
			return err
		}
	default:
		return fail(400, "不支持此财务操作")
	}
	c.Version++
	c.event(in.Action, u, note)
	if err = a.saveFinanceClaim(c); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": c})
	return nil
}
func financeFilters(r *http.Request) error {
	for _, k := range []string{"from", "to"} {
		if v := r.URL.Query().Get(k); v != "" && !financeDate(v) {
			return fail(400, "日期筛选格式应为 YYYY-MM-DD")
		}
	}
	if f, t := r.URL.Query().Get("from"), r.URL.Query().Get("to"); f != "" && t != "" && f > t {
		return fail(400, "开始日期不能晚于结束日期")
	}
	return nil
}
func financeMatches(r *http.Request, date, text string) bool {
	q := r.URL.Query()
	return (q.Get("from") == "" || date >= q.Get("from")) && (q.Get("to") == "" || date <= q.Get("to")) && (q.Get("q") == "" || strings.Contains(strings.ToLower(text), strings.ToLower(q.Get("q"))))
}
func (a *App) filteredFinanceClaims(r *http.Request, u User) ([]FinanceClaim, error) {
	if err := financeFilters(r); err != nil {
		return nil, err
	}
	q := "SELECT payload FROM finance_claims"
	args := []any{}
	if u.Role != "admin" || r.URL.Query().Get("scope") == "mine" {
		q += " WHERE user_id=?"
		args = append(args, u.ID)
	}
	all, err := readAll[FinanceClaim](a.store, q, args...)
	if err != nil {
		return nil, err
	}
	out := []FinanceClaim{}
	for _, c := range all {
		if status := r.URL.Query().Get("status"); status != "" && status != c.Status {
			continue
		}
		if financeMatches(r, c.IncurredOn, c.Title+" "+c.UserName+" "+c.Category+" "+c.Notes) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}
func (a *App) listFinanceClaims(w http.ResponseWriter, r *http.Request, u User) error {
	items, err := a.filteredFinanceClaims(r, u)
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"items": items})
	return nil
}
func (a *App) saveFinanceLedger(e FinanceLedgerEntry) error {
	var claim any
	if e.ClaimID != "" {
		claim = e.ClaimID
	}
	return a.store.exec("INSERT INTO finance_ledger(id,claim_id,payload) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", e.ID, claim, encoded(e))
}
func (a *App) createFinanceLedger(w http.ResponseWriter, r *http.Request, u User) error {
	if err := financeAdmin(u); err != nil {
		return err
	}
	var f FinanceLedgerFields
	if err := decode(r, &f); err != nil {
		return err
	}
	f.Category = strings.TrimSpace(f.Category)
	f.Counterparty = strings.TrimSpace(f.Counterparty)
	f.Note = strings.TrimSpace(f.Note)
	if (f.Direction != "income" && f.Direction != "expense") || !financeAmount(f.AmountCents) || !financePastDate(f.OccurredOn) || !financeText(f.Category, 120, true) || !financeText(f.Counterparty, 240, true) || !financeText(f.Note, 12000, false) {
		return fail(400, "请填写有效的收支方向、金额、已发生日期、类别及往来方")
	}
	scoped, tx, err := a.beginFinanceWrite(r)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a = scoped
	if f.FileIDs == nil {
		f.FileIDs = []string{}
	}
	if err = a.financeFiles(u, f.FileIDs, u.ID); err != nil {
		return err
	}
	e := FinanceLedgerEntry{FinanceLedgerFields: f, ID: randomID(), CreatedAt: now(), CreatedBy: u.ID, CreatedByName: u.Name}
	if err = a.saveFinanceLedger(e); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, 201, map[string]any{"item": e})
	return nil
}
func (a *App) reverseFinanceLedger(w http.ResponseWriter, r *http.Request, u User) error {
	if err := financeAdmin(u); err != nil {
		return err
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !financeText(in.Reason, 4000, true) {
		return fail(400, "请填写作废原因")
	}
	scoped, tx, err := a.beginFinanceWrite(r)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a = scoped
	e, err := readOne[FinanceLedgerEntry](a.store, "SELECT payload FROM finance_ledger WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	if e.ClaimID != "" {
		return fail(409, "报销打款自动入账的记录不能作废")
	}
	if e.ReversedAt != "" {
		return fail(409, "此流水已经作废")
	}
	e.ReversedAt = now()
	e.ReversalReason = in.Reason
	e.ReversedBy = u.ID
	if err = a.saveFinanceLedger(e); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": e})
	return nil
}
func (a *App) financeLedger(r *http.Request) ([]FinanceLedgerEntry, FinanceSummary, error) {
	// The ledger and pending claims must come from the same snapshot: a payment
	// moves value between those sets atomically. Do not mix two committed states.
	if a.store.tx == nil {
		opts := &sql.TxOptions{ReadOnly: true}
		if a.store.postgres {
			opts.Isolation = sql.LevelRepeatableRead
		}
		tx, err := a.store.db.BeginTx(r.Context(), opts)
		if err != nil {
			return nil, FinanceSummary{}, err
		}
		defer tx.Rollback()
		s := &Store{db: a.store.db, tx: tx, postgres: a.store.postgres, fileDir: a.store.fileDir}
		return (&App{store: s, config: a.config}).financeLedger(r)
	}
	sum := FinanceSummary{}
	if err := financeFilters(r); err != nil {
		return nil, sum, err
	}
	all, err := readAll[FinanceLedgerEntry](a.store, "SELECT payload FROM finance_ledger")
	if err != nil {
		return nil, sum, err
	}
	items := []FinanceLedgerEntry{}
	for _, e := range all {
		if e.ReversedAt == "" {
			if e.Direction == "income" {
				sum.IncomeCents += e.AmountCents
			} else {
				sum.ExpenseCents += e.AmountCents
			}
		}
		if d := r.URL.Query().Get("direction"); d != "" && d != e.Direction {
			continue
		}
		if financeMatches(r, e.OccurredOn, e.Counterparty+" "+e.Category+" "+e.Note) {
			items = append(items, e)
		}
	}
	claims, err := readAll[FinanceClaim](a.store, "SELECT payload FROM finance_claims")
	if err != nil {
		return nil, sum, err
	}
	for _, c := range claims {
		if c.Status == "approved" {
			sum.PendingCents += c.AmountCents
		}
	}
	sum.BalanceCents = sum.IncomeCents - sum.ExpenseCents
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredOn == items[j].OccurredOn {
			return items[i].CreatedAt > items[j].CreatedAt
		}
		return items[i].OccurredOn > items[j].OccurredOn
	})
	return items, sum, nil
}
func (a *App) listFinanceLedger(w http.ResponseWriter, r *http.Request, u User) error {
	if err := financeAdmin(u); err != nil {
		return err
	}
	items, sum, err := a.financeLedger(r)
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"items": items, "summary": sum})
	return nil
}
func financeMoney(c int64) string { return fmt.Sprintf("%d.%02d", c/100, c%100) }
func financeCSV(rows [][]string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&b)
	w.UseCRLF = true
	for _, row := range rows {
		for i, v := range row {
			trim := strings.TrimLeftFunc(v, unicode.IsSpace)
			if strings.HasPrefix(trim, "=") || strings.HasPrefix(trim, "+") || strings.HasPrefix(trim, "-") || strings.HasPrefix(trim, "@") {
				row[i] = "'" + v
			}
		}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return b.Bytes(), w.Error()
}
func financeDownload(w http.ResponseWriter, kind, name string) {
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
func financeStatus(s string) string {
	switch s {
	case "submitted":
		return "待审核"
	case "approved":
		return "待打款"
	case "returned":
		return "已退回"
	case "paid":
		return "已完成"
	case "cancelled":
		return "已撤销"
	}
	return s
}
func financeFolder(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
		if b.Len() >= 90 {
			break
		}
	}
	if b.Len() == 0 {
		return "凭证"
	}
	return b.String()
}
func (a *App) exportFinanceClaims(w http.ResponseWriter, r *http.Request, u User) error {
	if err := financeAdmin(u); err != nil {
		return err
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "zip" {
		return fail(400, "导出格式应为 csv 或 zip")
	}
	items, err := a.filteredFinanceClaims(r, u)
	if err != nil {
		return err
	}
	if ids := r.URL.Query().Get("ids"); ids != "" {
		selected := map[string]bool{}
		for _, id := range strings.Split(ids, ",") {
			selected[id] = true
		}
		out := []FinanceClaim{}
		for _, c := range items {
			if selected[c.ID] {
				out = append(out, c)
			}
		}
		items = out
	}
	rows := [][]string{{"申请编号", "申请人", "标题", "金额（元）", "币种", "类别", "费用日期", "状态", "备注", "审核说明", "实际打款日期", "打款流水或说明", "提交时间", "版本", "发票文件ID", "支付记录文件ID", "打款凭证文件ID"}}
	for _, c := range items {
		rows = append(rows, []string{c.ID, c.UserName, c.Title, financeMoney(c.AmountCents), c.Currency, c.Category, c.IncurredOn, financeStatus(c.Status), c.Notes, c.ReviewNote, c.PaidOn, c.PaymentReference, c.CreatedAt, fmt.Sprint(c.Version), strings.Join(c.InvoiceFileIDs, ";"), strings.Join(c.PaymentFileIDs, ";"), strings.Join(c.PayoutFileIDs, ";")})
	}
	data, err := financeCSV(rows)
	if err != nil {
		return err
	}
	if format == "csv" {
		financeDownload(w, "text/csv; charset=utf-8", "报销记录.csv")
		_, err = w.Write(data)
		return err
	}
	if len(items) > 500 {
		return fail(400, "每次最多打包 500 笔申请，请缩小筛选范围")
	}
	type attachment struct{ ID, Name string }
	files := []attachment{}
	var total int64
	for _, c := range items {
		folder := financeFolder(c.UserName) + "_" + financeFolder(c.ID) + "/"
		groups := map[string][]string{"发票": c.InvoiceFileIDs, "支付记录": c.PaymentFileIDs, "打款凭证": c.PayoutFileIDs}
		for _, rev := range c.Revisions {
			groups[fmt.Sprintf("历史版本_%d_发票", rev.Version)] = rev.InvoiceFileIDs
			groups[fmt.Sprintf("历史版本_%d_支付记录", rev.Version)] = rev.PaymentFileIDs
		}
		for name, ids := range groups {
			for i, id := range ids {
				f, err := a.fileAsset(id)
				if err != nil {
					return err
				}
				if !validAssetName(f.Name) {
					return fail(400, "凭证文件名不正确")
				}
				total += f.Size
				if total > 250<<20 {
					return fail(400, "凭证超过 250 MB，请分批导出")
				}
				files = append(files, attachment{ID: id, Name: folder + name + fmt.Sprintf("/%02d_", i+1) + f.Name})
			}
		}
	}
	// Finish the archive before sending headers, so a missing file fails cleanly.
	tmp, err := os.CreateTemp("", "mr-finance-export-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	z := zip.NewWriter(tmp)
	part, err := z.Create("报销记录.csv")
	if err != nil {
		return err
	}
	if _, err = part.Write(data); err != nil {
		return err
	}
	for _, f := range files {
		src, _, err := a.openAsset(f.ID)
		if err != nil {
			return err
		}
		dst, e := z.Create(f.Name)
		if e == nil {
			_, e = io.Copy(dst, src)
		}
		src.Close()
		if e != nil {
			return e
		}
	}
	if err = z.Close(); err != nil {
		return err
	}
	if _, err = tmp.Seek(0, 0); err != nil {
		return err
	}
	financeDownload(w, "application/zip", "报销凭证.zip")
	http.ServeContent(w, r, "报销凭证.zip", time.Time{}, tmp)
	return nil
}
func (a *App) exportFinanceLedger(w http.ResponseWriter, r *http.Request, u User) error {
	if err := financeAdmin(u); err != nil {
		return err
	}
	items, _, err := a.financeLedger(r)
	if err != nil {
		return err
	}
	rows := [][]string{{"流水编号", "收支方向", "金额（元）", "发生日期", "类别", "往来方", "备注", "关联报销编号", "登记人", "登记时间", "作废时间", "作废原因"}}
	for _, e := range items {
		direction := "收入"
		if e.Direction == "expense" {
			direction = "支出"
		}
		rows = append(rows, []string{e.ID, direction, financeMoney(e.AmountCents), e.OccurredOn, e.Category, e.Counterparty, e.Note, e.ClaimID, e.CreatedByName, e.CreatedAt, e.ReversedAt, e.ReversalReason})
	}
	data, err := financeCSV(rows)
	if err != nil {
		return err
	}
	financeDownload(w, "text/csv; charset=utf-8", "财务账本.csv")
	_, err = w.Write(data)
	return err
}
