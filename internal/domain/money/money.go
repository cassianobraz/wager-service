package money

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
)

type Money struct {
	minorUnits int64
	currency   string
}

var (
	ErrEmptyAmount      = errors.New("money: amount must not be empty")
	ErrInvalidFormat    = errors.New("money: amount must match a plain decimal with exactly two fractional digits, e.g. \"25.00\"")
	ErrNegativeAmount   = errors.New("money: negative amounts are not accepted from external financial input")
	ErrInvalidCurrency  = errors.New("money: currency must be a 3-letter uppercase ISO 4217 code")
	ErrCurrencyMismatch = errors.New("money: operation requires matching currencies")
	ErrOverflow         = errors.New("money: operation overflows the minor-units representation")
)

var (
	amountPattern   = regexp.MustCompile(`^(-?)([0-9]+)\.([0-9]{2})$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

func Zero(currency string) (Money, error) {
	c, err := normalizeCurrency(currency)
	if err != nil {
		return Money{}, err
	}
	return Money{minorUnits: 0, currency: c}, nil
}

func FromMinorUnits(minorUnits int64, currency string) (Money, error) {
	c, err := normalizeCurrency(currency)
	if err != nil {
		return Money{}, err
	}
	return Money{minorUnits: minorUnits, currency: c}, nil
}

func Parse(amount, currency string) (Money, error) {
	c, err := normalizeCurrency(currency)
	if err != nil {
		return Money{}, err
	}
	if amount == "" {
		return Money{}, ErrEmptyAmount
	}

	matches := amountPattern.FindStringSubmatch(amount)
	if matches == nil {
		return Money{}, ErrInvalidFormat
	}
	sign, intPart, fracPart := matches[1], matches[2], matches[3]

	intVal, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("%w: %v", ErrOverflow, err)
	}
	fracVal, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("%w: %v", ErrInvalidFormat, err)
	}

	major, ok := checkedMul(intVal, 100)
	if !ok {
		return Money{}, ErrOverflow
	}
	minorUnits, ok := checkedAdd(major, fracVal)
	if !ok {
		return Money{}, ErrOverflow
	}
	if sign == "-" {
		minorUnits, ok = checkedNegate(minorUnits)
		if !ok {
			return Money{}, ErrOverflow
		}
	}

	return Money{minorUnits: minorUnits, currency: c}, nil
}

func ParseExternalAmount(amount, currency string) (Money, error) {
	m, err := Parse(amount, currency)
	if err != nil {
		return Money{}, err
	}
	if m.minorUnits < 0 {
		return Money{}, ErrNegativeAmount
	}
	return m, nil
}

func normalizeCurrency(currency string) (string, error) {
	if !currencyPattern.MatchString(currency) {
		return "", ErrInvalidCurrency
	}
	return currency, nil
}

func (m Money) Currency() string { return m.currency }

func (m Money) MinorUnits() int64 { return m.minorUnits }

func (m Money) IsZero() bool { return m.minorUnits == 0 }

func (m Money) IsNegative() bool { return m.minorUnits < 0 }

func (m Money) IsPositive() bool { return m.minorUnits > 0 }

func (m Money) sameCurrency(other Money) error {
	if m.currency != other.currency {
		return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	return nil
}

func (m Money) Add(other Money) (Money, error) {
	if err := m.sameCurrency(other); err != nil {
		return Money{}, err
	}
	sum, ok := checkedAdd(m.minorUnits, other.minorUnits)
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{minorUnits: sum, currency: m.currency}, nil
}

func (m Money) Subtract(other Money) (Money, error) {
	if err := m.sameCurrency(other); err != nil {
		return Money{}, err
	}
	neg, ok := checkedNegate(other.minorUnits)
	if !ok {
		return Money{}, ErrOverflow
	}
	diff, ok := checkedAdd(m.minorUnits, neg)
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{minorUnits: diff, currency: m.currency}, nil
}

func (m Money) Negate() (Money, error) {
	neg, ok := checkedNegate(m.minorUnits)
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{minorUnits: neg, currency: m.currency}, nil
}

func (m Money) Compare(other Money) (int, error) {
	if err := m.sameCurrency(other); err != nil {
		return 0, err
	}
	switch {
	case m.minorUnits < other.minorUnits:
		return -1, nil
	case m.minorUnits > other.minorUnits:
		return 1, nil
	default:
		return 0, nil
	}
}

func (m Money) Equals(other Money) bool {
	return m.currency == other.currency && m.minorUnits == other.minorUnits
}

func (m Money) String() string {
	sign := ""
	abs := m.minorUnits
	if abs < 0 {
		sign = "-"

		if abs == math.MinInt64 {
			return fmt.Sprintf("-%d.%02d", -(abs / 100), -(abs % 100))
		}
		abs = -abs
	}
	return fmt.Sprintf("%s%d.%02d", sign, abs/100, abs%100)
}

type moneyJSON struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(moneyJSON{Amount: m.String(), Currency: m.currency})
}

func (m *Money) UnmarshalJSON(data []byte) error {
	var raw moneyJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := Parse(raw.Amount, raw.Currency)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

func checkedAdd(a, b int64) (int64, bool) {
	sum := a + b
	if (b > 0 && sum < a) || (b < 0 && sum > a) {
		return 0, false
	}
	return sum, true
}

func checkedNegate(a int64) (int64, bool) {
	if a == math.MinInt64 {
		return 0, false
	}
	return -a, true
}

func checkedMul(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	result := a * b
	if result/b != a {
		return 0, false
	}
	return result, true
}
