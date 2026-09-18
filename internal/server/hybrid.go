package server

import (
	"net/http"

	"github.com/aenix-io/aeman/internal/server/apiv1"
)

// hybrid is the generated interface while the migration to strict methods is
// under way: it embeds the strict handler and sends the operations that are
// still written against the raw request to their old handler. It exists
// because the two cannot be mixed at the mux — a route registered by hand as
// well as through the generator panics at start-up — and a strict method
// cannot wrap a handler that writes its own response. It goes away with the
// last override.
type hybrid struct {
	apiv1.ServerInterface
	s *Server
}

func (h hybrid) AddDeadline(w http.ResponseWriter, r *http.Request) {
	h.s.handleAddDeadline(w, r)
}

func (h hybrid) DeleteDeadline(w http.ResponseWriter, r *http.Request) {
	h.s.handleDeleteDeadline(w, r)
}

func (h hybrid) MoveDeadline(w http.ResponseWriter, r *http.Request) {
	h.s.handleMoveDeadline(w, r)
}

func (h hybrid) AddEpic(w http.ResponseWriter, r *http.Request) {
	h.s.handleAddEpic(w, r)
}

func (h hybrid) DeleteEpic(w http.ResponseWriter, r *http.Request) {
	h.s.handleDeleteEpic(w, r)
}

func (h hybrid) RenameEpic(w http.ResponseWriter, r *http.Request) {
	h.s.handleRenameEpic(w, r)
}

func (h hybrid) ReorderEpics(w http.ResponseWriter, r *http.Request) {
	h.s.handleReorderEpics(w, r)
}

func (h hybrid) SetEpicProject(w http.ResponseWriter, r *http.Request) {
	h.s.handleSetEpicProject(w, r)
}

func (h hybrid) SetPresence(w http.ResponseWriter, r *http.Request) {
	h.s.handleSetPresence(w, r)
}

func (h hybrid) ListProcesses(w http.ResponseWriter, r *http.Request, _ apiv1.ListProcessesParams) {
	h.s.handleListProcesses(w, r)
}

func (h hybrid) AddProcess(w http.ResponseWriter, r *http.Request) {
	h.s.handleAddProcess(w, r)
}

func (h hybrid) DeleteProcess(w http.ResponseWriter, r *http.Request) {
	h.s.handleDeleteProcess(w, r)
}

func (h hybrid) RenameProcess(w http.ResponseWriter, r *http.Request) {
	h.s.handleRenameProcess(w, r)
}

func (h hybrid) ReorderProcesses(w http.ResponseWriter, r *http.Request) {
	h.s.handleReorderProcesses(w, r)
}

func (h hybrid) SetProcessPaused(w http.ResponseWriter, r *http.Request) {
	h.s.handleSetProcessPaused(w, r)
}

func (h hybrid) SetProcessProject(w http.ResponseWriter, r *http.Request) {
	h.s.handleSetProcessProject(w, r)
}

func (h hybrid) AddTask(w http.ResponseWriter, r *http.Request) {
	h.s.handleAddTask(w, r)
}

func (h hybrid) ReorderProcessTasks(w http.ResponseWriter, r *http.Request) {
	h.s.handleReorderProcessTasks(w, r)
}

func (h hybrid) DeleteTask(w http.ResponseWriter, r *http.Request, _ apiv1.UID) {
	h.s.handleDeleteTask(w, r)
}

func (h hybrid) PatchTask(w http.ResponseWriter, r *http.Request, _ apiv1.UID) {
	h.s.handlePatchTask(w, r)
}

func (h hybrid) AddProject(w http.ResponseWriter, r *http.Request) {
	h.s.handleAddProject(w, r)
}

func (h hybrid) DeleteProject(w http.ResponseWriter, r *http.Request) {
	h.s.handleDeleteProject(w, r)
}

func (h hybrid) RenameProject(w http.ResponseWriter, r *http.Request) {
	h.s.handleRenameProject(w, r)
}

func (h hybrid) ReorderProjects(w http.ResponseWriter, r *http.Request) {
	h.s.handleReorderProjects(w, r)
}
