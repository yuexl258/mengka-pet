import { get, post } from './client'

export interface Wallet { coins: number; updated_at: string }
export interface WalletTransaction { id: number; amount: number; balance_after: number; transaction_type: string; reference_id: string; description: string; created_at: string }
export interface TransactionPage { items: WalletTransaction[]; total: number; page: number; page_size: number }
export const getWallet = () => get<Wallet>('/api/wallet')
export const getTransactions = (page = 1, pageSize = 20) => get<TransactionPage>(`/api/wallet/transactions?page=${page}&page_size=${pageSize}`)
export const adjustWallet = (userID: number, amount: number, description: string) => post<Wallet>(`/api/admin/users/${userID}/wallet/adjust`, { amount, description })
export interface RedeemResult { coins: number; updated_at: string; amount: number }
export interface GenerateCodesResult { amount: number; quantity: number; codes: string[] }
export const redeemCode = (code: string) => post<RedeemResult>('/api/wallet/redeem', { code })
export const generateCodes = (amount: number, quantity: number) => post<GenerateCodesResult>('/api/admin/redeem-codes', { amount, quantity })
export const transferCoins = (recipient_id: number, recipient_name: string, amount: number) => post<Wallet>('/api/wallet/transfer', { recipient_id, recipient_name, amount })
