package ralphloop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func detectOutputMode(args []string, stdout io.Writer) OutputFormat {
	for index := 0; index < len(args); index++ {
		if args[index] != "--output" {
			continue
		}
		if index+1 < len(args) {
			if format, err := parseOutputFormat(args[index+1]); err == nil {
				return format
			}
		}
	}
	if isTTYWriter(stdout) {
		return OutputText
	}
	return OutputJSON
}

func parseOutputFormat(value string) (OutputFormat, error) {
	switch OutputFormat(strings.ToLower(strings.TrimSpace(value))) {
	case OutputText:
		return OutputText, nil
	case OutputJSON:
		return OutputJSON, nil
	case OutputNDJSON:
		return OutputNDJSON, nil
	default:
		return "", fmt.Errorf("invalid value for --output: %s", value)
	}
}

func isTTYWriter(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func renderCommandError(format OutputFormat, stdout io.Writer, stderr io.Writer, command string, err *commandError) {
	if format == OutputText {
		target := stderr
		if target == nil {
			target = stdout
		}
		_, _ = fmt.Fprintln(target, err.Message)
		return
	}
	payload := map[string]any{
		"command": command,
		"status":  "failed",
		"error":   err,
	}
	if format == OutputNDJSON {
		_ = writeJSONLine(stdout, payload)
		return
	}
	_ = writeJSON(stdout, payload)
}

func normalizeCommandError(err error, fallbackCode string) *commandError {
	if err == nil {
		return nil
	}
	var structured *commandError
	if ok := asCommandError(err, &structured); ok {
		return structured
	}
	if IsUsageError(err) {
		return &commandError{Code: "usage", Message: err.Error()}
	}
	code := fallbackCode
	if strings.TrimSpace(code) == "" {
		code = "error"
	}
	return &commandError{Code: code, Message: err.Error()}
}

func asCommandError(err error, target **commandError) bool {
	var typed *commandError
	ok := errors.As(err, &typed)
	if !ok {
		return false
	}
	*target = typed
	return true
}

type renderedError struct {
	cause error
}

func (err *renderedError) Error() string {
	return err.cause.Error()
}

func (err *renderedError) Unwrap() error {
	return err.cause
}

func markRendered(err error) error {
	if err == nil {
		return nil
	}
	return &renderedError{cause: err}
}

func isRenderedError(err error) bool {
	var target *renderedError
	return errors.As(err, &target)
}

type writerAdapter struct {
	target io.Writer
}

func (writer writerAdapter) WriteJSON(payload any) error {
	return writeJSON(writer.target, payload)
}

func (writer writerAdapter) WriteJSONLine(payload any) error {
	return writeJSONLine(writer.target, payload)
}

func writeJSON(writer io.Writer, payload any) error {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = writer.Write(encoded)
	return err
}

func writeJSONLine(writer io.Writer, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = writer.Write(encoded)
	return err
}
