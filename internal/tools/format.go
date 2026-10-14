package tools

import (
	"strings"
	"time"

	"github.com/prro-cloud/prro-go"
)

// Допоміжне форматування для текстових зведень. Структурований результат
// лишає значення такими, як їх повернув API: гроші — рядками.

// timePart — " <label> <момент у RFC 3339>" або "", якщо момент невідомий
// (нульовий): у зведенні модель не має прочитати відсутнє значення як факт.
func timePart(label string, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return " " + label + " " + t.Format(time.RFC3339)
}

// uah перетворює суму білінгу в копійках ("150.0000") на гривні для
// людини ("1.50 UAH"), переносячи кому в рядку, без арифметики з рухомою
// комою. Нечислове значення повертається як є з позначкою "kop".
func uah(k prro.Kopecks) string {
	s, sign := string(k), ""
	if rest, ok := strings.CutPrefix(s, "-"); ok {
		s, sign = rest, "-"
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || !isDigits(whole) || !isDigits(frac) {
		return string(k) + " kop"
	}
	whole = strings.Repeat("0", max(3-len(whole), 0)) + whole
	w := strings.TrimLeft(whole[:len(whole)-2], "0")
	if w == "" {
		w = "0"
	}
	f := strings.TrimRight(whole[len(whole)-2:]+frac, "0")
	f += strings.Repeat("0", max(2-len(f), 0))
	return sign + w + "." + f + " UAH"
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// orDash — значення або "-", якщо воно порожнє.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
