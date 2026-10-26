package postgres

// jobModelTransitionExpression reads only verified linkage; raw Job arguments never establish public identity.
const jobModelTransitionExpression = `(SELECT jsonb_build_object('modelId',binding.embedding_model_id,
'selectionGeneration',binding.selection_generation,'jobId',jobs.id,'status',jobs.status,
'isCurrent',COALESCE(state.latest_transition_job_id=jobs.id AND state.selection_generation=binding.selection_generation
 AND settings.embedding_model_id=binding.embedding_model_id,false),
'canRetry',COALESCE(state.latest_transition_job_id=jobs.id AND state.selection_generation=binding.selection_generation
 AND settings.embedding_model_id=binding.embedding_model_id AND jobs.status IN ('dead','cancelled'),false))
FROM model_transition_jobs binding
LEFT JOIN workspace_retrieval_state state ON state.workspace_id=binding.workspace_id
LEFT JOIN workspace_settings settings ON settings.workspace_id=binding.workspace_id
WHERE binding.job_id=jobs.id AND binding.workspace_id=jobs.workspace_id)`

const latestModelTransitionExpression = `CASE WHEN binding.job_id IS NOT NULL THEN jsonb_build_object(
'modelId',binding.embedding_model_id,'selectionGeneration',binding.selection_generation,
'jobId',job.id,'status',job.status,'isCurrent',true,'canRetry',job.status IN ('dead','cancelled'))
WHEN state.workspace_id IS NOT NULL AND (state.serving_model_id IS DISTINCT FROM settings.embedding_model_id
 OR NOT COALESCE(progress.seed_complete,false)) THEN jsonb_build_object('modelId',settings.embedding_model_id,
'selectionGeneration',state.selection_generation,'jobId',NULL,'status','waiting','isCurrent',true,'canRetry',false) END`
