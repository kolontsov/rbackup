package config

import (
	"fmt"
	"os"
	"regexp"
)

var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ResolveRemoteSecrets resolves ${ENV_VAR} in a single remote's credential fields.
func ResolveRemoteSecrets(r *Remote) error {
	if r.AccessKeyID != "" {
		resolved, err := resolveRequired(r.AccessKeyID, fmt.Sprintf("remotes[%s].access_key_id", r.Name))
		if err != nil {
			return err
		}
		r.AccessKeyID = resolved
	}
	if r.SecretAccessKey != "" {
		resolved, err := resolveRequired(r.SecretAccessKey, fmt.Sprintf("remotes[%s].secret_access_key", r.Name))
		if err != nil {
			return err
		}
		r.SecretAccessKey = resolved
	}
	return nil
}

// ResolveSecrets resolves ${ENV_VAR} in secret-bearing fields.
// Remote credentials: unresolved → error.
// Notification URLs: unresolved → warn+clear (skip notification).
// Notification header values: unresolved → warn+remove header.
func ResolveSecrets(cfg *Config) error {
	for i := range cfg.Remotes {
		if err := ResolveRemoteSecrets(&cfg.Remotes[i]); err != nil {
			return err
		}
	}

	ResolveNotificationSecrets(&cfg.Notifications)
	return nil
}

// ResolveSelectedRemoteSecrets resolves ${ENV_VAR} only for the selected remote
// credential fields.
func ResolveSelectedRemoteSecrets(cfg *Config, remoteNames []string) error {
	seen := make(map[string]struct{}, len(remoteNames))
	for _, name := range remoteNames {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		remote, err := FindRemote(cfg, name)
		if err != nil {
			return err
		}
		if err := ResolveRemoteSecrets(remote); err != nil {
			return err
		}
	}
	return nil
}

// ResolveNotificationSecrets resolves optional notification secrets.
func ResolveNotificationSecrets(notif *Notifications) {
	if notif.SuccessURL != "" {
		resolved, ok := resolveOptional(notif.SuccessURL, "notifications.success_url")
		if !ok {
			notif.SuccessURL = ""
		} else {
			notif.SuccessURL = resolved
		}
	}

	if notif.FailureURL != "" {
		resolved, ok := resolveOptional(notif.FailureURL, "notifications.failure_url")
		if !ok {
			notif.FailureURL = ""
		} else {
			notif.FailureURL = resolved
		}
	}

	for key, val := range notif.Headers {
		resolved, ok := resolveOptional(val, fmt.Sprintf("notifications.headers[%s]", key))
		if !ok {
			delete(notif.Headers, key)
		} else {
			notif.Headers[key] = resolved
		}
	}
}

func resolveRequired(s, field string) (string, error) {
	var resolveErr error
	result := envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		varName := envVarRe.FindStringSubmatch(match)[1]
		val, ok := os.LookupEnv(varName)
		if !ok {
			resolveErr = fmt.Errorf("%s: environment variable %q not set", field, varName)
			return match
		}
		return val
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return result, nil
}

func resolveOptional(s, field string) (string, bool) {
	allResolved := true
	result := envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		varName := envVarRe.FindStringSubmatch(match)[1]
		val, ok := os.LookupEnv(varName)
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: %s: environment variable %q not set, skipping\n", field, varName)
			allResolved = false
			return match
		}
		return val
	})
	if !allResolved {
		return "", false
	}
	return result, true
}
