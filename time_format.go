package main

import (
	"regexp"
	"strconv"
	"strings"
)

var utcOffsetRe = regexp.MustCompile(`^UTC([+-])(\d{1,2})$`)

func parseUTCOffsetHours(tz string) (int, bool) {
	tz = strings.TrimSpace(tz)
	tz = strings.ToUpper(tz)
	match := utcOffsetRe.FindStringSubmatch(tz)
	if len(match) != 3 {
		return 0, false
	}

	hours, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, false
	}
	if hours > 14 {
		// keep this sane; real-world offsets are within roughly [-12, +14]
		return 0, false
	}

	if match[1] == "-" {
		hours = -hours
	}
	return hours, true
}

func normalizeUserTimeLayout(layout string) string {
	layout = strings.TrimSpace(layout)
	if layout == "" {
		return ""
	}

	if strings.Contains(layout, "2006") {
		return layout
	}

	lower := strings.ToLower(layout)
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return layout
	}

	hasAmPm := false
	if fields[len(fields)-1] == "a" {
		hasAmPm = true
		fields = fields[:len(fields)-1]
	}

	if len(fields) < 1 || len(fields) > 2 {
		return layout
	}

	datePart := ""
	timePart := ""
	if len(fields) == 1 {
		part := fields[0]
		if strings.Contains(part, ":") {
			timePart = part
		} else {
			datePart = part
		}
	} else {
		datePart = fields[0]
		timePart = fields[1]
		if strings.Contains(datePart, ":") || !strings.Contains(timePart, ":") {
			return layout
		}
	}

	parseDate := func(s string) (string, bool) {
		if s == "" {
			return "", true
		}
		var b strings.Builder
		replaced := false
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case 'y':
				b.WriteString("2006")
				replaced = true
			case 'm':
				b.WriteString("01")
				replaced = true
			case 'd':
				b.WriteString("02")
				replaced = true
			default:
				b.WriteByte(s[i])
			}
		}
		return b.String(), replaced
	}

	parseTime := func(s string) (string, bool) {
		if s == "" {
			return "", true
		}
		var b strings.Builder
		replaced := false
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case 'h':
				b.WriteString("15")
				replaced = true
			case 'm':
				b.WriteString("04")
				replaced = true
			case 's':
				b.WriteString("05")
				replaced = true
			default:
				b.WriteByte(s[i])
			}
		}
		return b.String(), replaced
	}

	dateLayout, dateOk := parseDate(datePart)
	timeLayout, timeOk := parseTime(timePart)
	if !dateOk || !timeOk {
		return layout
	}
	if dateLayout == "" && timeLayout == "" {
		return layout
	}

	out := ""
	if dateLayout != "" && timeLayout != "" {
		out = dateLayout + " " + timeLayout
	} else if dateLayout != "" {
		out = dateLayout
	} else {
		out = timeLayout
	}

	if hasAmPm {
		out += " PM"
	}
	return out
}
