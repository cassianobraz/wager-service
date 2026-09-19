package money_test

import (
	"encoding/json"
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"math"
	"testing"
)

func mustParse(t *testing.T, amount, currency string) money.Money {
	t.Helper()
	m, err := money.Parse(amount, currency)
	if err != nil {
		t.Fatalf("Parse(%q, %q) unexpected error: %v", amount, currency, err)
	}
	return m
}

func TestParse_ValidAmounts(t *testing.T) {
	cases := []struct {
		amount string
		want   int64
	}{
		{"0.00", 0},
		{"25.00", 2500},
		{"0.01", 1},
		{"1000.00", 100000},
		{"-5.00", -500},
	}
	for _, tc := range cases {
		m := mustParse(t, tc.amount, "BRL")
		if m.MinorUnits() != tc.want {
			t.Errorf("Parse(%q) minor units = %d, want %d", tc.amount, m.MinorUnits(), tc.want)
		}
		if m.String() != tc.amount {
			t.Errorf("Parse(%q).String() = %q, want %q", tc.amount, m.String(), tc.amount)
		}
	}
}

func TestParse_RejectsInvalidInputs(t *testing.T) {
	cases := map[string]string{
		"empty string":         "",
		"NaN":                  "NaN",
		"Infinity":             "Infinity",
		"scientific notation":  "2.5e1",
		"excess scale":         "25.001",
		"missing decimals":     "25",
		"single decimal digit": "25.0",
		"only a dot":           ".",
		"letters":              "abc",
		"trailing whitespace":  "25.00 ",
		"leading whitespace":   " 25.00",
		"plus sign":            "+25.00",
		"double dot":           "25..00",
		"comma decimal":        "25,00",
	}
	for name, amount := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := money.Parse(amount, "BRL"); err == nil {
				t.Errorf("Parse(%q) expected error, got nil", amount)
			}
		})
	}
}

func TestParse_RejectsInvalidCurrency(t *testing.T) {
	cases := []string{"", "brl", "BR", "BRLL", "123"}
	for _, currency := range cases {
		if _, err := money.Parse("25.00", currency); !errors.Is(err, money.ErrInvalidCurrency) {
			t.Errorf("Parse(_, %q) error = %v, want ErrInvalidCurrency", currency, err)
		}
	}
}

func TestParse_AllowsNegative(t *testing.T) {
	m := mustParse(t, "-25.00", "BRL")
	if !m.IsNegative() {
		t.Fatal("expected Parse to allow a negative amount")
	}
}

func TestParseExternalAmount_RejectsNegative(t *testing.T) {
	_, err := money.ParseExternalAmount("-25.00", "BRL")
	if !errors.Is(err, money.ErrNegativeAmount) {
		t.Fatalf("expected ErrNegativeAmount, got %v", err)
	}
}

func TestParseExternalAmount_AcceptsZeroAndPositive(t *testing.T) {
	if _, err := money.ParseExternalAmount("0.00", "BRL"); err != nil {
		t.Errorf("unexpected error for zero: %v", err)
	}
	if _, err := money.ParseExternalAmount("25.00", "BRL"); err != nil {
		t.Errorf("unexpected error for positive: %v", err)
	}
}

func TestParse_OverflowOnHugeIntegerPart(t *testing.T) {
	_, err := money.Parse("99999999999999999999999.00", "BRL")
	if !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("expected ErrOverflow, got %v", err)
	}
}

func TestZero(t *testing.T) {
	z, err := money.Zero("BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !z.IsZero() {
		t.Fatal("expected Zero() to be zero")
	}
	if z.String() != "0.00" {
		t.Fatalf("Zero().String() = %q, want 0.00", z.String())
	}
}

func TestAdd_SameCurrency(t *testing.T) {
	a := mustParse(t, "25.00", "BRL")
	b := mustParse(t, "10.50", "BRL")
	sum, err := a.Add(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum.String() != "35.50" {
		t.Fatalf("sum = %s, want 35.50", sum)
	}
}

func TestAdd_CurrencyMismatch(t *testing.T) {
	a := mustParse(t, "25.00", "BRL")
	b := mustParse(t, "10.00", "USD")
	if _, err := a.Add(b); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestSubtract_CanProduceNegativeResult(t *testing.T) {
	a := mustParse(t, "10.00", "BRL")
	b := mustParse(t, "25.00", "BRL")
	diff, err := a.Subtract(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff.String() != "-15.00" {
		t.Fatalf("diff = %s, want -15.00", diff)
	}
}

func TestSubtract_CurrencyMismatch(t *testing.T) {
	a := mustParse(t, "25.00", "BRL")
	b := mustParse(t, "10.00", "USD")
	if _, err := a.Subtract(b); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestNegate(t *testing.T) {
	a := mustParse(t, "25.00", "BRL")
	neg, err := a.Negate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if neg.String() != "-25.00" {
		t.Fatalf("neg = %s, want -25.00", neg)
	}
	back, err := neg.Negate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !back.Equals(a) {
		t.Fatalf("double negation = %s, want %s", back, a)
	}
}

func TestNegate_OverflowAtMinInt64(t *testing.T) {
	m, err := money.FromMinorUnits(math.MinInt64, "BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := m.Negate(); !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("expected ErrOverflow negating MinInt64, got %v", err)
	}
}

func TestAdd_OverflowAtMaxInt64(t *testing.T) {
	a, err := money.FromMinorUnits(math.MaxInt64, "BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := money.FromMinorUnits(1, "BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := a.Add(b); !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("expected ErrOverflow, got %v", err)
	}
}

func TestCompare(t *testing.T) {
	a := mustParse(t, "10.00", "BRL")
	b := mustParse(t, "25.00", "BRL")

	if cmp, err := a.Compare(b); err != nil || cmp != -1 {
		t.Fatalf("a.Compare(b) = (%d, %v), want (-1, nil)", cmp, err)
	}
	if cmp, err := b.Compare(a); err != nil || cmp != 1 {
		t.Fatalf("b.Compare(a) = (%d, %v), want (1, nil)", cmp, err)
	}
	if cmp, err := a.Compare(a); err != nil || cmp != 0 {
		t.Fatalf("a.Compare(a) = (%d, %v), want (0, nil)", cmp, err)
	}
}

func TestCompare_CurrencyMismatch(t *testing.T) {
	a := mustParse(t, "25.00", "BRL")
	b := mustParse(t, "10.00", "USD")
	if _, err := a.Compare(b); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestEquals_NeverErrorsAcrossCurrencies(t *testing.T) {
	a := mustParse(t, "25.00", "BRL")
	b := mustParse(t, "25.00", "USD")
	if a.Equals(b) {
		t.Fatal("expected amounts in different currencies to not be equal")
	}
}

func TestJSON_MarshalUnmarshalRoundTrip(t *testing.T) {
	original := mustParse(t, "975.00", "BRL")
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if string(raw) != `{"amount":"975.00","currency":"BRL"}` {
		t.Fatalf("unexpected JSON: %s", raw)
	}

	var decoded money.Money
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !decoded.Equals(original) {
		t.Fatalf("round trip mismatch: got %s, want %s", decoded, original)
	}
}

func TestJSON_UnmarshalRejectsInvalidAmount(t *testing.T) {
	var m money.Money
	err := json.Unmarshal([]byte(`{"amount":"NaN","currency":"BRL"}`), &m)
	if !errors.Is(err, money.ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got %v", err)
	}
}

func TestFromMinorUnits_RejectsInvalidCurrency(t *testing.T) {
	if _, err := money.FromMinorUnits(100, "brl"); !errors.Is(err, money.ErrInvalidCurrency) {
		t.Fatalf("expected ErrInvalidCurrency, got %v", err)
	}
}

func TestCurrency_Getter(t *testing.T) {
	m := mustParse(t, "25.00", "BRL")
	if m.Currency() != "BRL" {
		t.Fatalf("Currency() = %q, want BRL", m.Currency())
	}
}

func TestZero_RejectsInvalidCurrency(t *testing.T) {
	if _, err := money.Zero("brl"); !errors.Is(err, money.ErrInvalidCurrency) {
		t.Fatalf("expected ErrInvalidCurrency, got %v", err)
	}
}

func TestParseExternalAmount_RejectsInvalidCurrency(t *testing.T) {
	if _, err := money.ParseExternalAmount("25.00", "brl"); !errors.Is(err, money.ErrInvalidCurrency) {
		t.Fatalf("expected ErrInvalidCurrency, got %v", err)
	}
}

func TestSubtract_OverflowNegatingMinInt64Other(t *testing.T) {
	a := mustParse(t, "0.00", "BRL")
	b, err := money.FromMinorUnits(math.MinInt64, "BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := a.Subtract(b); !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("expected ErrOverflow, got %v", err)
	}
}

func TestString_MinInt64EdgeCase(t *testing.T) {
	m, err := money.FromMinorUnits(math.MinInt64, "BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.String() != "-92233720368547758.08" {
		t.Fatalf("String() = %q, want -92233720368547758.08", m.String())
	}
}

func TestJSON_UnmarshalRejectsMalformedJSON(t *testing.T) {
	var m money.Money
	if err := json.Unmarshal([]byte(`not json`), &m); err == nil {
		t.Fatal("expected an error unmarshaling malformed JSON")
	}
}

func TestIsPositiveIsZeroIsNegative(t *testing.T) {
	pos := mustParse(t, "1.00", "BRL")
	zero := mustParse(t, "0.00", "BRL")
	neg := mustParse(t, "-1.00", "BRL")

	if !pos.IsPositive() || pos.IsZero() || pos.IsNegative() {
		t.Fatal("positive amount classified incorrectly")
	}
	if zero.IsPositive() || !zero.IsZero() || zero.IsNegative() {
		t.Fatal("zero amount classified incorrectly")
	}
	if neg.IsPositive() || neg.IsZero() || !neg.IsNegative() {
		t.Fatal("negative amount classified incorrectly")
	}
}
