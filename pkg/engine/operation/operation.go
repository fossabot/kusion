package operation

import (
	"fmt"
	"sync"

	"kusionstack.io/kusion/pkg/engine/models"
	"kusionstack.io/kusion/pkg/engine/runtime"
	"kusionstack.io/kusion/pkg/util/kdump"

	"github.com/jinzhu/copier"

	"kusionstack.io/kusion/pkg/engine/states"
	"kusionstack.io/kusion/pkg/log"
	"kusionstack.io/kusion/pkg/util"
)

// Operation contains values that is useful for all operations
type Operation struct {
	// OperationType represents the Type of this operation
	OperationType Type

	// StateStorage represents the storage where state will be saved during this operation
	StateStorage states.StateStorage

	// CtxResourceIndex represents resources updated by this operation
	CtxResourceIndex map[string]*models.Resource

	// PriorStateResourceIndex represents resource state saved during the last operation
	PriorStateResourceIndex map[string]*models.Resource

	// StateResourceIndex represents resources that will be saved in states.StateStorage
	StateResourceIndex map[string]*models.Resource

	// Order is resources' change order during this operation
	Order *ChangeOrder

	// Runtime is the resource infrastructure runtime of this operation
	Runtime runtime.Runtime

	// MsgCh is used to send operation status like Success, Failed or Skip to Kusion CTl,
	// and this message will be displayed in the terminal
	MsgCh chan Message

	// resultState is the final State build by this operation, and this State will be saved in the StateStorage
	resultState *states.State

	// lock is the operation-wide mutex
	lock *sync.Mutex
}

type Message struct {
	ResourceID string   // ResourceNode.ID()
	OpResult   OpResult // Success/Failed/Skip
	OpErr      error    // Operate error detail
}

type Request struct {
	Tenant   string       `json:"tenant"`
	Stack    string       `json:"stack"`
	Project  string       `json:"project"`
	Operator string       `json:"operator"`
	Manifest *models.Spec `json:"models"`
}

type OpResult string

// OpResult values
const (
	Success OpResult = "Success"
	Failed  OpResult = "Failed"
	Skip    OpResult = "Skip"
)

// RefreshResourceIndex refresh resources in CtxResourceIndex & StateResourceIndex
func (o *Operation) RefreshResourceIndex(resourceKey string, resource *models.Resource, actionType ActionType) error {
	o.lock.Lock()
	defer o.lock.Unlock()

	switch actionType {
	case Delete:
		o.CtxResourceIndex[resourceKey] = nil
		o.StateResourceIndex[resourceKey] = nil
	case Create, Update:
		o.CtxResourceIndex[resourceKey] = resource
		o.StateResourceIndex[resourceKey] = resource
	default:
		panic("unsupported actionType:" + actionType.Ing())
	}
	return nil
}

func initStates(storage states.StateStorage, request *Request) (*states.State, *states.State) {
	latestState, err := storage.GetLatestState(
		&states.StateQuery{
			Tenant:  request.Tenant,
			Stack:   request.Stack,
			Project: request.Project,
		},
	)
	util.CheckNotError(err, fmt.Sprintf("GetLatestState failed with request: %v", kdump.FormatN(request)))
	if latestState == nil {
		log.Infof("can't find states with request: %v", kdump.FormatN(request))
		latestState = states.NewState()
	}
	resultState := states.NewState()
	resultState.Serial = latestState.Serial
	err = copier.Copy(resultState, request)
	util.CheckNotError(err, "Copy request to resultState, request")
	resultState.Resources = nil

	return latestState, resultState
}

func (o *Operation) UpdateState(resourceIndex map[string]*models.Resource) error {
	o.lock.Lock()
	defer o.lock.Unlock()

	state := o.resultState
	state.Serial += 1
	state.Resources = nil

	res := make([]models.Resource, len(resourceIndex))
	for key := range resourceIndex {
		// {key -> nil} represents Deleted action
		if resourceIndex[key] == nil {
			continue
		}
		res = append(res, *resourceIndex[key])
	}

	state.Resources = res
	err := o.StateStorage.Apply(state)
	if err != nil {
		return fmt.Errorf("insert priorState failed. %w", err)
	}
	log.Infof("UpdateState:%v success", state.ID)
	return nil
}
