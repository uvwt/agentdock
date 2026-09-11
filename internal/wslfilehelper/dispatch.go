//go:build linux

package wslfilehelper

func Dispatch(req *Request) (*Response, error) {
	return dispatchWithOps(req, defaultTransactionOps())
}

func dispatchWithOps(req *Request, ops transactionOps) (*Response, error) {
	if req == nil {
		return nil, fail("INVALID_ARGUMENT", "WSL file helper request is required", nil)
	}
	switch req.Action {
	case "read":
		return readText(req.Path, req.RejectSymlink, req.AllowMissing)
	case "list_dir":
		return listDirectory(req)
	case "search_text":
		return searchText(req)
	case "write_atomic":
		return atomicWrite(req)
	case "delete":
		return deleteFile(req)
	case "move":
		return moveFile(req)
	case "patch_transaction":
		return patchTransaction(req, ops)
	case "recover_patch_transactions":
		return recoverPatchTransactions(req, ops)
	default:
		return nil, fail("INVALID_ACTION", "unsupported WSL file helper action", map[string]any{"action": req.Action})
	}
}
