export type FinanceStatus = 'submitted' | 'approved' | 'returned' | 'paid' | 'cancelled';
export interface FinanceEvent { action: string; actorName: string; at: string; note?: string }
export interface FinanceClaimRevision {
  title: string; amountCents: number; currency: 'CNY'; category: string; incurredOn: string;
  notes: string; invoiceFileIds: string[]; paymentFileIds: string[]; version: number; savedAt: string;
}
export interface FinanceClaim {
  id: string; userId: string; userName: string; title: string; amountCents: number;
  currency: 'CNY'; category: string; incurredOn: string; notes: string;
  invoiceFileIds: string[]; paymentFileIds: string[]; status: FinanceStatus; version: number;
  createdAt: string; updatedAt: string; reviewNote?: string; paidAt?: string; paidOn?: string;
  paymentReference?: string; payoutFileIds?: string[]; events: FinanceEvent[]; revisions?: FinanceClaimRevision[];
}
export interface FinanceLedgerEntry {
  id: string; direction: 'income' | 'expense'; amountCents: number; occurredOn: string;
  category: string; counterparty: string; note: string; fileIds: string[]; claimId?: string;
  createdAt: string; createdByName: string; reversedAt?: string; reversalReason?: string;
}
export interface FinanceSummary { incomeCents: number; expenseCents: number; balanceCents: number; pendingCents: number }
export const financeStatusLabels: Record<FinanceStatus, string> = {
  submitted: '待审核', approved: '待付款', returned: '已退回', paid: '已完成', cancelled: '已撤回',
};
export function yuanToCents(value: string): number {
  const input = value.trim();
  if (!/^\d{1,9}(\.\d{1,2})?$/.test(input)) throw new Error('请输入大于 0 的金额，最多保留两位小数。');
  const [whole = '0', fraction = ''] = input.split('.');
  const amount = Number(whole) * 100 + Number(fraction.padEnd(2, '0'));
  if (!Number.isSafeInteger(amount) || amount <= 0) throw new Error('金额必须大于 0。');
  return amount;
}
export const financeMoney = (cents: number) => new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' }).format(cents / 100);
