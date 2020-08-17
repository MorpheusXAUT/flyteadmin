package mocks

import (
	"context"

	"github.com/lyft/flyteadmin/pkg/common"
	"github.com/lyft/flyteadmin/pkg/repositories/interfaces"
	"github.com/lyft/flyteadmin/pkg/repositories/models"
)

type CreateProjectFunction func(ctx context.Context, project models.Project) error
type GetProjectFunction func(ctx context.Context, projectID string) (models.Project, error)
type ListProjectsFunction func(ctx context.Context, sortParameter common.SortParameter) ([]models.Project, error)
type UpdateProjectFunction func(ctx context.Context, project models.Project, prevProject models.Project) (error)

type MockProjectRepo struct {
	CreateFunction       CreateProjectFunction
	GetFunction          GetProjectFunction
	ListProjectsFunction ListProjectsFunction
	UpdateProjectFunction UpdateProjectFunction
}

func (r *MockProjectRepo) Create(ctx context.Context, project models.Project) error {
	if r.CreateFunction != nil {
		return r.CreateFunction(ctx, project)
	}
	return nil
}

func (r *MockProjectRepo) Get(ctx context.Context, projectID string) (models.Project, error) {
	if r.GetFunction != nil {
		return r.GetFunction(ctx, projectID)
	}
	return models.Project{}, nil
}

func (r *MockProjectRepo) ListAll(ctx context.Context, sortParameter common.SortParameter) ([]models.Project, error) {
	if r.ListProjectsFunction != nil {
		return r.ListProjectsFunction(ctx, sortParameter)
	}
	return make([]models.Project, 0), nil
}

func (r *MockProjectRepo) UpdateProject(ctx context.Context, prevProject models.Project, updatedProject models.Project) (error) {
	if r.UpdateProjectFunction != nil {
		return r.UpdateProjectFunction(ctx, prevProject, updatedProject)
	}
	return nil
}

func NewMockProjectRepo() interfaces.ProjectRepoInterface {
	return &MockProjectRepo{}
}
