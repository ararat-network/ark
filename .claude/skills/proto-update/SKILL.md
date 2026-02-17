# Guided Proto File Changes

Handle protobuf file modifications with a structured workflow that prevents cascading errors.

## Workflow

### Phase 1: Plan (DO NOT edit files yet)
1. Read the target proto file(s) and understand current state
2. Read related proto files for context (imports, dependencies)
3. Explain the proposed changes clearly:
   - Which files will be modified
   - What will change in each file
   - How this affects generated Go code
   - Whether amino annotations are needed
4. **Stop and wait for user approval before proceeding**

### Phase 2: Apply (only after user approves)
1. Apply the proto file edits
2. Run buf lint to check for issues: `buf lint proto/`
3. Run buf generate: `buf generate`
4. Check if generated Go files need any manual fixups
5. Update amino codec registration in `x/market/types/codec.go` if new message types were added
6. Run `go build ./...` to verify everything compiles

### Phase 3: Verify
1. Show a summary of all files that were created or modified
2. Report any warnings or issues from buf or compilation
3. If anything failed, diagnose and fix before finishing

## Rules
- Never edit proto files without explaining the plan first
- Always run buf lint before buf generate
- Always verify compilation after proto changes
- If adding a new message type, ensure amino registration is included
