package gloq

import (
	"fmt"
	"reflect"
	"strings"
)

const maxErrorDepth = 32

type structuredError struct {
	Message string            `json:"message"`
	Type    string            `json:"type"`
	Causes  []structuredError `json:"causes,omitempty"`
	Stack   string            `json:"stack,omitempty"`
}

type prettyError struct {
	key string
	err error
}

func describeError(err error, includeStack bool, depth int) structuredError {
	detail := structuredError{
		Message: errorMessage(err),
		Type:    fmt.Sprintf("%T", err),
	}
	if includeStack {
		detail.Stack = errorStack(err)
	}
	if depth >= maxErrorDepth || isNilError(err) {
		return detail
	}
	for _, cause := range errorCauses(err) {
		detail.Causes = append(detail.Causes, describeError(cause, includeStack, depth+1))
	}
	return detail
}

func appendPrettyError(buf []byte, detail prettyError, includeStack, color bool) []byte {
	causes := errorCauses(detail.err)
	keyColor := ""
	if color {
		keyColor = errorKeyColor
	}
	buf = writeErrorLine(buf, "  ", detail.key, keyColor, errorDisplayMessage(detail.err, causes), true)
	buf = appendPrettyCauses(buf, causes, "    ", includeStack, color, 0)
	if includeStack {
		if stack := errorStack(detail.err); stack != "" {
			buf = writeErrorLine(buf, "    ", "stack", detailColor(color), stack, false)
		}
	}
	return buf
}

func appendPrettyCauses(buf []byte, causes []error, indent string, includeStack, color bool, depth int) []byte {
	if depth >= maxErrorDepth {
		return buf
	}
	for index, cause := range causes {
		label := "caused by"
		if len(causes) > 1 {
			label = fmt.Sprintf("caused by[%d]", index)
		}
		childCauses := errorCauses(cause)
		buf = writeErrorLine(buf, indent, label, detailColor(color), errorDisplayMessage(cause, childCauses), false)
		if includeStack {
			if stack := errorStack(cause); stack != "" {
				buf = writeErrorLine(buf, indent+"  ", "stack", detailColor(color), stack, false)
			}
		}
		buf = appendPrettyCauses(buf, childCauses, indent+"  ", includeStack, color, depth+1)
	}
	return buf
}

func detailColor(color bool) string {
	if color {
		return faintColor
	}
	return ""
}

func errorDisplayMessage(err error, causes []error) string {
	message := errorMessage(err)
	if len(causes) == 1 {
		cause := errorMessage(causes[0])
		if own := strings.TrimSuffix(message, ": "+cause); own != message && own != "" {
			return own
		}
	}
	if len(causes) > 1 {
		messages := make([]string, 0, len(causes))
		for _, cause := range causes {
			messages = append(messages, errorMessage(cause))
		}
		if message == strings.Join(messages, "\n") {
			return fmt.Sprintf("%d errors", len(causes))
		}
	}
	return message
}

// writeErrorLine writes "label: message", indenting continuation lines so a
// multi-line message cannot be mistaken for a new record.
func writeErrorLine(buf []byte, indent, label, labelColor, message string, quoteLabel bool) []byte {
	buf = append(buf, indent...)
	buf = append(buf, labelColor...)
	if quoteLabel {
		buf = appendKey(buf, nil, label)
	} else {
		buf = append(buf, label...)
	}
	if labelColor != "" {
		buf = append(buf, resetColor...)
	}
	buf = append(buf, ": "...)
	for {
		line, rest, found := strings.Cut(message, "\n")
		buf = appendText(buf, line)
		buf = append(buf, '\n')
		if !found {
			return buf
		}
		buf = append(buf, indent...)
		buf = append(buf, "  "...)
		message = rest
	}
}

func errorCauses(err error) []error {
	if err == nil || isNilError(err) {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := unwrapMany(joined)
		result := make([]error, 0, len(causes))
		for _, cause := range causes {
			if cause != nil {
				result = append(result, cause)
			}
		}
		return result
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if cause := unwrapOne(wrapped); cause != nil {
			return []error{cause}
		}
	}
	return nil
}

func unwrapMany(err interface{ Unwrap() []error }) (causes []error) {
	defer func() {
		if recover() != nil {
			causes = nil
		}
	}()
	return err.Unwrap()
}

func unwrapOne(err interface{ Unwrap() error }) (cause error) {
	defer func() {
		if recover() != nil {
			cause = nil
		}
	}()
	return err.Unwrap()
}

func errorMessage(err error) (message string) {
	if err == nil || isNilError(err) {
		return "<nil>"
	}
	defer func() {
		if recover() != nil {
			message = "<error message panicked>"
		}
	}()
	return err.Error()
}

func errorStack(err error) string {
	if err == nil || isNilError(err) {
		return ""
	}
	formatted := fmt.Sprintf("%+v", err)
	if formatted == errorMessage(err) || !strings.Contains(formatted, "\n") {
		return ""
	}
	return formatted
}

func isNilError(err error) bool {
	if err == nil {
		return true
	}
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
