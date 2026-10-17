package sdkruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type AppIdentity struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Query string `json:"query"`
}

// ApplicationDriver resolves without effects and revalidates identity before launching.
// LaunchApp must report possible effects once native launch has been dispatched.
type ApplicationDriver interface {
	ResolveAppIdentity(context.Context, string) (AppIdentity, error)
	LaunchApp(context.Context, AppIdentity) error
}

type resolveAppInput struct {
	Query            string  `json:"query"`
	Launch           *bool   `json:"launch"`
	ExpectedIdentity *string `json:"expectedIdentity"`
}

func validAppQuery(query string) bool {
	return strings.TrimSpace(query) != "" && utf8.RuneCountInString(query) <= 512 && !strings.ContainsRune(query, 0)
}

func (d *Desktop) runningApps(ctx context.Context) ([]App, error) {
	value, err := d.Call(ctx, "listApps", json.RawMessage(`{}`))
	if err != nil {
		return nil, err
	}
	return value.([]App), nil
}

func uniqueApp(apps []App, matches func(App) bool) (*App, error) {
	var found *App
	for _, app := range apps {
		if matches(app) {
			if found != nil {
				return nil, Error("TARGET_UNAVAILABLE", "Application is ambiguous; use an ID from listApps")
			}
			copy := app
			found = &copy
		}
	}
	return found, nil
}

func checkAppIdentity(actual string, expected *string) error {
	if expected != nil && actual != *expected {
		return Error("TARGET_UNAVAILABLE", "Application identity changed before launch")
	}
	return nil
}

func (d *Desktop) resolveApp(ctx context.Context, input resolveAppInput) (any, error) {
	query := strings.TrimSpace(input.Query)
	apps, err := d.runningApps(ctx)
	if err != nil {
		return nil, err
	}
	app, err := uniqueApp(apps, func(app App) bool { return app.ID == query || strings.EqualFold(app.Name, query) })
	if err != nil {
		return nil, err
	}
	if app != nil {
		return *app, checkAppIdentity(app.Identity, input.ExpectedIdentity)
	}
	launcher, supported := d.driver.(ApplicationDriver)
	if !supported {
		if input.Launch != nil && !*input.Launch {
			return nil, Error("TARGET_UNAVAILABLE", "Application is no longer running; bind it explicitly to launch again")
		}
		return nil, Error("UNSUPPORTED_CAPABILITY", "Application is not running; launching applications is unavailable on this platform")
	}
	identity, err := launcher.ResolveAppIdentity(ctx, query)
	if err != nil {
		return nil, err
	}
	if err := checkAppIdentity(identity.ID, input.ExpectedIdentity); err != nil {
		return nil, err
	}
	app, err = uniqueApp(apps, func(app App) bool { return app.Identity == identity.ID })
	if err != nil {
		return nil, err
	}
	if app != nil {
		return *app, nil
	}
	if input.Launch != nil && !*input.Launch {
		return nil, Error("TARGET_UNAVAILABLE", "Application is no longer running; bind it explicitly to launch again")
	}
	if ctx.Err() != nil {
		return nil, Error("CANCELLED", "Application launch cancelled before dispatch")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := launcher.LaunchApp(ctx, identity); err != nil {
		return nil, err
	}
	for {
		apps, err = d.runningApps(ctx)
		if err == nil {
			app, err = uniqueApp(apps, func(app App) bool { return app.Identity == identity.ID })
			if err != nil {
				return nil, &DomainError{Code: "TARGET_UNAVAILABLE", Message: err.Error(), Effect: "possible"}
			}
			if app != nil {
				screenshot := false
				_, err = d.driver.Observe(ctx, d.apps[app.ID], ObserveOptions{IncludeScreenshot: &screenshot, Activation: "never", MaxTreeNodes: 1, MaxTreeDepth: 1, TextLimit: float64(1)})
				if err == nil && ctx.Err() == nil {
					return *app, nil
				}
			}
		}
		if err != nil && ctx.Err() == nil {
			var domain *DomainError
			if !errors.As(err, &domain) || domain.Code != "TARGET_UNAVAILABLE" {
				if domain != nil {
					return nil, &DomainError{Code: domain.Code, Message: domain.Message, Effect: "possible"}
				}
				return nil, &DomainError{Code: "TARGET_UNAVAILABLE", Message: err.Error(), Effect: "possible"}
			}
		}
		select {
		case <-ctx.Done():
			code := "TIMEOUT"
			if ctx.Err() == context.Canceled {
				code = "CANCELLED"
			}
			return nil, &DomainError{Code: code, Message: "Application launch was dispatched but no matching accessible window became ready; inspect running applications before retrying", Effect: "possible"}
		case <-time.After(100 * time.Millisecond):
		}
	}
}
