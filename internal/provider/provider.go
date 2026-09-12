// Package provider implements an experimental, read-only Xiaoyuzhou adapter.
package provider

import (
	"context"
	"starling/internal/model"
)

type Provider interface {
	SendCode(context.Context, string, string) error
	Login(context.Context, string, string, string) (model.Credentials, model.Identity, error)
	Me(context.Context, string) (model.Identity, error)
	Refresh(context.Context, model.Credentials) (model.Credentials, error)
	List(context.Context, string, string, string, string) (model.Page, error)
	Detail(context.Context, string, string, string) (model.Item, error)
}
