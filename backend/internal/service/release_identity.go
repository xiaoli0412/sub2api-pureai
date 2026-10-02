package service

// PureAIReleaseRepository is the GitHub repository used for release metadata,
// online updates, rollback candidates, and release assets. Keep this identity
// separate from the upstream update implementation so upstream merges do not
// silently redirect installed servers to another publisher.
const PureAIReleaseRepository = "xiaoli0412/sub2api-pureai"

// githubRepo is retained as the local alias used by the update service.
const githubRepo = PureAIReleaseRepository
