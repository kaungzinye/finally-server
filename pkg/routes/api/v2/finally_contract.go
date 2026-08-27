// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package apiv2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/web/handler"

	"github.com/danielgtaylor/huma/v2"
)

const FinallyContractPath = "/finally/openapi.json"

func RegisterFinallyRoutes(api huma.API) {
	tags := []string{"finally"}
	Register(api, huma.Operation{
		OperationID:   "finally-auth-login",
		Summary:       "Login to Finally",
		Method:        http.MethodPost,
		Path:          "/finally/login",
		DefaultStatus: http.StatusOK,
		Tags:          tags,
		Security:      publicSecurity,
	}, authLogin)
	Register(api, huma.Operation{
		OperationID: "finally-tasks-list",
		Summary:     "List Finally tasks",
		Description: "Returns the authenticated user's tasks in one project, paginated and flat.",
		Method:      http.MethodGet,
		Path:        "/finally/projects/{project}/tasks",
		Tags:        tags,
	}, projectTasksList)
	Register(api, huma.Operation{
		OperationID: "finally-tasks-create",
		Summary:     "Create a Finally task",
		Method:      http.MethodPost,
		Path:        "/finally/projects/{project}/tasks",
		Tags:        tags,
	}, tasksCreate)
	Register(api, huma.Operation{
		OperationID: "finally-tasks-read",
		Summary:     "Get a Finally task",
		Method:      http.MethodGet,
		Path:        "/finally/tasks/{projecttask}",
		Tags:        tags,
	}, tasksRead)
	Register(api, huma.Operation{
		OperationID: "finally-tasks-update",
		Summary:     "Update a Finally task",
		Method:      http.MethodPut,
		Path:        "/finally/tasks/{projecttask}",
		Tags:        tags,
	}, tasksUpdate)
	Register(api, huma.Operation{
		OperationID: "finally-tasks-delete",
		Summary:     "Delete a Finally task",
		Method:      http.MethodDelete,
		Path:        "/finally/tasks/{projecttask}",
		Tags:        tags,
	}, tasksDelete)
	Register(api, huma.Operation{
		OperationID:   "finally-tasks-complete",
		Summary:       "Complete a Finally task",
		Description:   "Marks the task complete while preserving every other task field.",
		Method:        http.MethodPost,
		Path:          "/finally/tasks/{projecttask}/complete",
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, finallyTasksComplete)
	RegisterFinallyCalendarRoutes(api)

	contract, err := finallyClientContract(api.OpenAPI())
	if err != nil {
		panic(err)
	}
	encoded, err := json.Marshal(contract)
	if err != nil {
		panic(fmt.Errorf("marshal Finally client contract: %w", err))
	}

	Register(api, huma.Operation{
		OperationID:   "finally-contract",
		Summary:       "Get the Finally client contract",
		Method:        http.MethodGet,
		Path:          FinallyContractPath,
		DefaultStatus: http.StatusOK,
		Tags:          tags,
		Security:      publicSecurity,
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The narrow Finally client OpenAPI contract.",
				Content: map[string]*huma.MediaType{
					"application/openapi+json": {
						Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"},
					},
				},
			},
		},
	}, func(context.Context, *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", "application/openapi+json")
			_, _ = ctx.BodyWriter().Write(encoded)
		}}, nil
	})
}

func finallyClientContract(source *huma.OpenAPI) (*huma.OpenAPI, error) {
	if source.Info == nil || source.Components == nil {
		return nil, fmt.Errorf("build Finally client contract: incomplete source contract")
	}
	jwtAuthSource := source.Components.SecuritySchemes["JWTKeyAuth"]
	apiTokenAuth := source.Components.SecuritySchemes["APITokenAuth"]
	if jwtAuthSource == nil || apiTokenAuth == nil {
		return nil, fmt.Errorf("build Finally client contract: required security schemes are missing")
	}
	login := source.Paths["/finally/login"]
	projectTasks := source.Paths["/finally/projects/{project}/tasks"]
	task := source.Paths["/finally/tasks/{projecttask}"]
	complete := source.Paths["/finally/tasks/{projecttask}/complete"]
	calendarAccounts := source.Paths["/finally/calendar/accounts"]
	calendarAccount := source.Paths["/finally/calendar/accounts/{account}"]
	calendarContext := source.Paths["/finally/calendar/context"]
	if login == nil || login.Post == nil || projectTasks == nil || projectTasks.Get == nil || projectTasks.Post == nil ||
		task == nil || task.Get == nil || task.Put == nil || task.Delete == nil ||
		complete == nil || complete.Post == nil || calendarAccounts == nil ||
		calendarAccounts.Post == nil || calendarAccounts.Get == nil || calendarAccount == nil ||
		calendarAccount.Delete == nil || calendarContext == nil || calendarContext.Post == nil {
		return nil, fmt.Errorf("build Finally client contract: required lifecycle operation is missing")
	}

	info := *source.Info
	info.Title = "Finally Client API"
	info.Description = "The authenticated task and planning-context API used by Finally iOS and automation clients."

	paths := map[string]*huma.PathItem{
		"/finally/login": {
			Post: login.Post,
		},
		"/finally/projects/{project}/tasks": {
			Get:  projectTasks.Get,
			Post: projectTasks.Post,
		},
		"/finally/tasks/{projecttask}": {
			Get:    task.Get,
			Put:    task.Put,
			Delete: task.Delete,
		},
		"/finally/tasks/{projecttask}/complete": {
			Post: complete.Post,
		},
		"/finally/calendar/accounts": {
			Get:  calendarAccounts.Get,
			Post: calendarAccounts.Post,
		},
		"/finally/calendar/accounts/{account}": {
			Delete: calendarAccount.Delete,
		},
		"/finally/calendar/context": {
			Post: calendarContext.Post,
		},
	}
	components, err := finallyClientComponents(source.Components, paths)
	if err != nil {
		return nil, err
	}
	jwtAuth := *jwtAuthSource
	jwtAuth.Description = "User session JWT issued via /api/v2/finally/login."
	components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"JWTKeyAuth":   &jwtAuth,
		"APITokenAuth": apiTokenAuth,
	}

	return &huma.OpenAPI{
		OpenAPI:           source.OpenAPI,
		Info:              &info,
		JSONSchemaDialect: source.JSONSchemaDialect,
		Servers:           source.Servers,
		Paths:             paths,
		Components:        components,
		Security:          source.Security,
	}, nil
}

type finallyComponentRef struct {
	kind string
	name string
}

func finallyClientComponents(source *huma.Components, paths map[string]*huma.PathItem) (*huma.Components, error) {
	components := &huma.Components{
		Schemas: huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer),
	}
	queue, err := finallyComponentRefs(paths)
	if err != nil {
		return nil, fmt.Errorf("build Finally client contract: discover operation references: %w", err)
	}
	seen := make(map[finallyComponentRef]bool, len(queue))
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		if seen[ref] {
			continue
		}
		seen[ref] = true

		component, ok := copyFinallyComponent(components, source, ref)
		if !ok {
			return nil, fmt.Errorf("build Finally client contract: unresolved component reference #/components/%s/%s", ref.kind, ref.name)
		}
		references, err := finallyComponentRefs(component)
		if err != nil {
			return nil, fmt.Errorf("build Finally client contract: discover references from #/components/%s/%s: %w", ref.kind, ref.name, err)
		}
		queue = append(queue, references...)
		sort.Slice(queue, func(i, j int) bool {
			if queue[i].kind == queue[j].kind {
				return queue[i].name < queue[j].name
			}
			return queue[i].kind < queue[j].kind
		})
	}
	return components, nil
}

func finallyComponentRefs(value any) ([]finallyComponentRef, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return nil, err
	}

	refs := map[finallyComponentRef]bool{}
	var walk func(any)
	walk = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "$ref" {
					ref, ok := child.(string)
					if !ok {
						continue
					}
					const prefix = "#/components/"
					path, ok := strings.CutPrefix(ref, prefix)
					if !ok {
						continue
					}
					parts := strings.SplitN(path, "/", 2)
					if len(parts) != 2 {
						continue
					}
					unescape := strings.NewReplacer("~1", "/", "~0", "~").Replace
					refs[finallyComponentRef{kind: unescape(parts[0]), name: unescape(parts[1])}] = true
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(document)

	result := make([]finallyComponentRef, 0, len(refs))
	for ref := range refs {
		result = append(result, ref)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].kind == result[j].kind {
			return result[i].name < result[j].name
		}
		return result[i].kind < result[j].kind
	})
	return result, nil
}

func copyFinallyComponent(target, source *huma.Components, ref finallyComponentRef) (any, bool) {
	switch ref.kind {
	case "schemas":
		component, ok := source.Schemas.Map()[ref.name]
		if ok {
			target.Schemas.Map()[ref.name] = component
		}
		return component, ok
	case "responses":
		component, ok := source.Responses[ref.name]
		if ok {
			if target.Responses == nil {
				target.Responses = map[string]*huma.Response{}
			}
			target.Responses[ref.name] = component
		}
		return component, ok
	case "parameters":
		component, ok := source.Parameters[ref.name]
		if ok {
			if target.Parameters == nil {
				target.Parameters = map[string]*huma.Param{}
			}
			target.Parameters[ref.name] = component
		}
		return component, ok
	case "examples":
		component, ok := source.Examples[ref.name]
		if ok {
			if target.Examples == nil {
				target.Examples = map[string]*huma.Example{}
			}
			target.Examples[ref.name] = component
		}
		return component, ok
	case "requestBodies":
		component, ok := source.RequestBodies[ref.name]
		if ok {
			if target.RequestBodies == nil {
				target.RequestBodies = map[string]*huma.RequestBody{}
			}
			target.RequestBodies[ref.name] = component
		}
		return component, ok
	case "headers":
		component, ok := source.Headers[ref.name]
		if ok {
			if target.Headers == nil {
				target.Headers = map[string]*huma.Header{}
			}
			target.Headers[ref.name] = component
		}
		return component, ok
	case "securitySchemes":
		component, ok := source.SecuritySchemes[ref.name]
		if ok {
			if target.SecuritySchemes == nil {
				target.SecuritySchemes = map[string]*huma.SecurityScheme{}
			}
			target.SecuritySchemes[ref.name] = component
		}
		return component, ok
	case "links":
		component, ok := source.Links[ref.name]
		if ok {
			if target.Links == nil {
				target.Links = map[string]*huma.Link{}
			}
			target.Links[ref.name] = component
		}
		return component, ok
	case "callbacks":
		component, ok := source.Callbacks[ref.name]
		if ok {
			if target.Callbacks == nil {
				target.Callbacks = map[string]*huma.PathItem{}
			}
			target.Callbacks[ref.name] = component
		}
		return component, ok
	case "pathItems":
		component, ok := source.PathItems[ref.name]
		if ok {
			if target.PathItems == nil {
				target.PathItems = map[string]*huma.PathItem{}
			}
			target.PathItems[ref.name] = component
		}
		return component, ok
	default:
		return nil, false
	}
}

func finallyTasksComplete(ctx context.Context, in *struct {
	ID int64 `path:"projecttask" doc:"The numeric id of the task to complete."`
}) (*singleBody[models.Task], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := handler.DoReadOne(ctx, &models.Task{ID: in.ID}, a); err != nil {
		return nil, translateDomainError(err)
	}
	update := &models.BulkTask{
		TaskIDs: []int64{in.ID},
		Fields:  []string{"done"},
		Values:  &models.Task{Done: true},
	}
	if err := handler.DoUpdate(ctx, update, a); err != nil {
		return nil, translateDomainError(err)
	}
	if len(update.Tasks) != 1 {
		return nil, huma.Error500InternalServerError("completion returned an unexpected task count")
	}
	return &singleBody[models.Task]{Body: update.Tasks[0]}, nil
}
