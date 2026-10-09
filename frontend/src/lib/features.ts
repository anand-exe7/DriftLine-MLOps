// The loan-default model takes 24 *encoded* features. People think in the 16
// raw application fields, so the UI collects those and encodes them here
// exactly like ml/projects/loan_default/preprocess.py does:
//   Yes/No -> 1/0, categoricals -> one-hot with the first (alphabetical) level dropped.

import type { BaselineStats } from "./api";

export type RawApplicant = {
  Age: number;
  Income: number;
  LoanAmount: number;
  CreditScore: number;
  MonthsEmployed: number;
  NumCreditLines: number;
  InterestRate: number;
  LoanTerm: number;
  DTIRatio: number;
  HasMortgage: boolean;
  HasDependents: boolean;
  HasCoSigner: boolean;
  Education: string;
  EmploymentType: string;
  MaritalStatus: string;
  LoanPurpose: string;
};

export interface NumericField {
  key: keyof RawApplicant;
  label: string;
  help: string;
  step: number;
  format: (v: number) => string;
}

const money = (v: number) => `$${Math.round(v).toLocaleString()}`;

export const NUMERIC_FIELDS: NumericField[] = [
  { key: "Age", label: "Age", help: "Applicant age in years", step: 1, format: (v) => `${v} yrs` },
  { key: "Income", label: "Annual income", help: "Gross yearly income", step: 1000, format: money },
  { key: "LoanAmount", label: "Loan amount", help: "Principal requested", step: 1000, format: money },
  { key: "CreditScore", label: "Credit score", help: "FICO-style score", step: 1, format: (v) => `${v}` },
  { key: "MonthsEmployed", label: "Months employed", help: "Tenure at current job", step: 1, format: (v) => `${v} mo` },
  { key: "NumCreditLines", label: "Open credit lines", help: "Number of active credit lines", step: 1, format: (v) => `${v}` },
  { key: "InterestRate", label: "Interest rate", help: "Offered APR", step: 0.01, format: (v) => `${v.toFixed(2)}%` },
  { key: "DTIRatio", label: "Debt-to-income", help: "Monthly debt / monthly income", step: 0.01, format: (v) => v.toFixed(2) },
];

export const LOAN_TERMS = [12, 24, 36, 48, 60];

export const CATEGORICAL: Record<"Education" | "EmploymentType" | "MaritalStatus" | "LoanPurpose", { label: string; options: string[] }> = {
  Education: { label: "Education", options: ["Bachelor's", "High School", "Master's", "PhD"] },
  EmploymentType: { label: "Employment", options: ["Full-time", "Part-time", "Self-employed", "Unemployed"] },
  MaritalStatus: { label: "Marital status", options: ["Divorced", "Married", "Single"] },
  LoanPurpose: { label: "Loan purpose", options: ["Auto", "Business", "Education", "Home", "Other"] },
};

export const FLAGS: { key: "HasMortgage" | "HasDependents" | "HasCoSigner"; label: string }[] = [
  { key: "HasMortgage", label: "Has mortgage" },
  { key: "HasDependents", label: "Has dependents" },
  { key: "HasCoSigner", label: "Has co-signer" },
];

/** Real applicants from the held-out test split, with their true outcome. */
export const TEST_APPLICANTS: { id: string; defaulted: boolean; raw: RawApplicant }[] = [
  {
    id: "2M13A9C7TR", defaulted: true,
    raw: { Age: 18, Income: 112573, LoanAmount: 239768, CreditScore: 593, MonthsEmployed: 23, NumCreditLines: 3, InterestRate: 10.52, LoanTerm: 12, DTIRatio: 0.65, Education: "Master's", EmploymentType: "Unemployed", MaritalStatus: "Married", HasMortgage: true, HasDependents: false, LoanPurpose: "Business", HasCoSigner: false },
  },
  {
    id: "CY9I51VNH3", defaulted: true,
    raw: { Age: 41, Income: 24849, LoanAmount: 246658, CreditScore: 732, MonthsEmployed: 102, NumCreditLines: 4, InterestRate: 11.33, LoanTerm: 36, DTIRatio: 0.26, Education: "Bachelor's", EmploymentType: "Unemployed", MaritalStatus: "Divorced", HasMortgage: false, HasDependents: true, LoanPurpose: "Business", HasCoSigner: false },
  },
  {
    id: "SJJNN2DFZ7", defaulted: false,
    raw: { Age: 54, Income: 20956, LoanAmount: 87156, CreditScore: 789, MonthsEmployed: 102, NumCreditLines: 4, InterestRate: 8.99, LoanTerm: 24, DTIRatio: 0.65, Education: "Master's", EmploymentType: "Full-time", MaritalStatus: "Single", HasMortgage: true, HasDependents: true, LoanPurpose: "Education", HasCoSigner: false },
  },
  {
    id: "GOC4OI55T8", defaulted: false,
    raw: { Age: 49, Income: 138499, LoanAmount: 227279, CreditScore: 603, MonthsEmployed: 102, NumCreditLines: 4, InterestRate: 2.46, LoanTerm: 12, DTIRatio: 0.67, Education: "High School", EmploymentType: "Full-time", MaritalStatus: "Married", HasMortgage: true, HasDependents: false, LoanPurpose: "Home", HasCoSigner: false },
  },
];

/** Range for a numeric field: min/max from the training baseline when available. */
export function rangeFor(key: string, baseline?: BaselineStats | null): { min: number; max: number } {
  const f = baseline?.features?.[key];
  if (f) return { min: f.min, max: f.max };
  const fallback: Record<string, { min: number; max: number }> = {
    Age: { min: 18, max: 69 }, Income: { min: 15000, max: 150000 }, LoanAmount: { min: 5000, max: 250000 },
    CreditScore: { min: 300, max: 850 }, MonthsEmployed: { min: 0, max: 119 }, NumCreditLines: { min: 1, max: 4 },
    InterestRate: { min: 2, max: 25 }, DTIRatio: { min: 0.1, max: 0.9 },
  };
  return fallback[key] ?? { min: 0, max: 100 };
}

/** Training-set median per numeric field — a neutral starting applicant. */
export function medianApplicant(baseline?: BaselineStats | null): RawApplicant {
  const med = (k: string, fb: number) => baseline?.features?.[k]?.deciles?.p50 ?? fb;
  return {
    Age: med("Age", 43), Income: med("Income", 82466), LoanAmount: med("LoanAmount", 127000),
    CreditScore: med("CreditScore", 574), MonthsEmployed: med("MonthsEmployed", 60),
    NumCreditLines: med("NumCreditLines", 2), InterestRate: med("InterestRate", 13.5),
    LoanTerm: 36, DTIRatio: med("DTIRatio", 0.5),
    HasMortgage: false, HasDependents: false, HasCoSigner: false,
    Education: "Bachelor's", EmploymentType: "Full-time", MaritalStatus: "Married", LoanPurpose: "Auto",
  };
}

/**
 * Encodes a raw applicant into the model's feature map, driven by the
 * feature names in the model's own feature_schema.json — so the encoding
 * follows whatever version is selected rather than a hard-coded list.
 */
export function encode(raw: RawApplicant, featureNames: string[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const name of featureNames) {
    if (name in raw) {
      const v = raw[name as keyof RawApplicant];
      out[name] = typeof v === "boolean" ? (v ? 1 : 0) : Number(v);
      continue;
    }
    const i = name.indexOf("_");
    const col = name.slice(0, i) as keyof RawApplicant;
    const level = name.slice(i + 1);
    out[name] = raw[col] === level ? 1 : 0;
  }
  return out;
}
