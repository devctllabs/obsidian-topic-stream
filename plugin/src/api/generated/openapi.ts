/** Generated from api/openapi/openapi.yaml. Do not edit. */
export interface paths {
    "/api/v1/system/status": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Inspect daemon readiness */
        get: operations["getSystemStatus"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/events": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Stream advisory invalidation events */
        get: operations["streamEvents"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/admin/embedding-models": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List registered embedding models */
        get: operations["listEmbeddingModels"];
        put?: never;
        /** Register and initialize an embedding model */
        post: operations["createEmbeddingModel"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/admin/embedding-models/{modelId}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                modelId: components["parameters"]["model-id"];
            };
            cookie?: never;
        };
        /** Get an embedding model */
        get: operations["getEmbeddingModel"];
        /** Update mutable embedding model fields */
        put: operations["updateEmbeddingModel"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/admin/embedding-models/{modelId}/initialize": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                modelId: components["parameters"]["model-id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Retry embedding model initialization */
        post: operations["initializeEmbeddingModel"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/embedding-model-options": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List embedding models visible to workspace settings */
        get: operations["listEmbeddingModelOptions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/retrieval-status": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Inspect desired, serving, and transition retrieval state */
        get: operations["getRetrievalStatus"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspace-settings": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Read non-secret workspace settings */
        get: operations["getWorkspaceSettings"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /** Update non-secret workspace settings */
        patch: operations["updateWorkspaceSettings"];
        trace?: never;
    };
    "/api/v1/preference-profile": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Read current preference guidance */
        get: operations["getPreferenceProfile"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /** Update preference guidance */
        patch: operations["updatePreferenceProfile"];
        trace?: never;
    };
    "/api/v1/sources": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listSources"];
        put?: never;
        post: operations["createSource"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/sources/sync": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["syncAllSources"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/sources/{sourceId}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                sourceId: components["parameters"]["source-id"];
            };
            cookie?: never;
        };
        get: operations["getSource"];
        put?: never;
        post?: never;
        delete: operations["deleteSource"];
        options?: never;
        head?: never;
        patch: operations["updateSource"];
        trace?: never;
    };
    "/api/v1/sources/{sourceId}/sync": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                sourceId: components["parameters"]["source-id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["syncSource"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/topics": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listTopics"];
        put?: never;
        post: operations["createTopic"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/topics/{topicId}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                topicId: components["parameters"]["topic-id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        delete: operations["deleteTopic"];
        options?: never;
        head?: never;
        patch: operations["updateTopic"];
        trace?: never;
    };
    "/api/v1/feed": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Read saved Feed publications without ranking or publishing */
        get: operations["getFeed"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/feed/refresh": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Publish a bounded batch from ready candidates and read the latest saved page
         * @description Does not wait for candidate preparation. Published Items never publish again in this Workspace. Automatic network retries are disabled.
         */
        post: operations["refreshFeed"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/search": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["searchItems"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/items/{itemId}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        get: operations["getItem"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/items/{itemId}/ai-summary": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        /** Read AI annotation state without starting generation */
        get: operations["getItemSummary"];
        put?: never;
        /** Explicitly request asynchronous AI summary of current owned content */
        post: operations["requestItemSummary"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/digest-issues/{issueId}/items/{itemId}/ai-summary": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                issueId: components["parameters"]["issue-id"];
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Explicitly request AI summary of the exact saved Item snapshot */
        post: operations["requestDigestItemSummary"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/items/{itemId}/feedback": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        get?: never;
        put: operations["setItemFeedback"];
        post?: never;
        delete: operations["clearItemFeedback"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/items/{itemId}/reading-state": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        get?: never;
        put: operations["setItemReadingState"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/items/{itemId}/visibility": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        get?: never;
        put: operations["setItemVisibility"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/items/{itemId}/topic-corrections": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        get?: never;
        put: operations["setItemTopicCorrection"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/items/{itemId}/images/{imageId}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
                imageId: components["parameters"]["image-id"];
            };
            cookie?: never;
        };
        /** Download an image already associated with an item */
        get: operations["downloadItemImage"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/digests": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listDigests"];
        put?: never;
        post: operations["createDigest"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/digests/{digestId}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                digestId: components["parameters"]["digest-id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        delete: operations["deleteDigest"];
        options?: never;
        head?: never;
        patch: operations["updateDigest"];
        trace?: never;
    };
    "/api/v1/digests/{digestId}/generate": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                digestId: components["parameters"]["digest-id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["generateDigest"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/digest-issues": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listDigestIssues"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/digest-issues/{issueId}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                issueId: components["parameters"]["issue-id"];
            };
            cookie?: never;
        };
        get: operations["getDigestIssue"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/jobs": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listJobs"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/jobs/{jobId}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                jobId: components["parameters"]["job-id"];
            };
            cookie?: never;
        };
        get: operations["getJob"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/jobs/{jobId}/retry": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                jobId: components["parameters"]["job-id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["retryJob"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/jobs/{jobId}/cancel": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                jobId: components["parameters"]["job-id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Request cancellation of a queued or running job */
        post: operations["cancelJob"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/export": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["exportWorkspace"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/import": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["importWorkspace"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        Problem: {
            /**
             * Format: uri-reference
             * @description Stable problem type identifier. `about:blank` is used only when the HTTP status fully describes the problem.
             */
            type: string;
            /** @description Stable summary associated with the problem type. */
            title: string;
            status: number;
            /** @description Safe English explanation approved for display to the user. Never a raw internal error string. */
            detail?: string;
            /** Format: uri-reference */
            instance?: string;
            /** @description Stable snake_case application error code. Unexpected application failures use internal_error with HTTP 500; clients must not infer a code from title or detail. */
            code?: string;
            errors?: {
                /** @description Application field path, or the JSON pointer or parameter name supplied by OpenAPI validation. */
                field: string;
                /** @description Safe English explanation of the violated constraint, without the submitted value. */
                message: string;
            }[];
        };
        ProjectionPendingProblem: {
            /**
             * Format: uri-reference
             * @constant
             */
            type: "urn:topic-stream:problem:projection-pending";
            /** @constant */
            title: "Retrieval projection pending";
            /** @constant */
            status: 503;
            detail?: string;
            /** @constant */
            code: "projection_pending";
            /** @constant */
            canonicalCommitted: true;
            projectionRevision: components["schemas"]["UUID"];
        };
        SystemStatus: {
            /** @enum {string} */
            status: "ready" | "degraded" | "unavailable";
            version: string;
            workspaceId: components["schemas"]["UUID"];
            components: components["schemas"]["ComponentStatus"][];
            now: components["schemas"]["Timestamp"];
        };
        WorkspaceSettings: {
            version: number;
            embeddingModelId: components["schemas"]["UUID"];
            generation: components["schemas"]["GenerationSettings"];
            summary: components["schemas"]["SummarySettings"];
            instructions: components["schemas"]["TaskInstructions"];
            retention: components["schemas"]["RetentionSettings"];
            retrieval: components["schemas"]["RetrievalSettings"];
            updatedAt: components["schemas"]["Timestamp"];
        };
        WorkspaceSettingsPatch: {
            embeddingModelId?: components["schemas"]["UUID"];
            generation?: components["schemas"]["GenerationSettings"];
            summary?: components["schemas"]["SummarySettings"];
            instructions?: components["schemas"]["TaskInstructions"];
            retention?: components["schemas"]["RetentionSettings"];
            retrieval?: components["schemas"]["RetrievalSettings"];
        };
        EmbeddingModelDescriptor: {
            name: string;
            /** Format: uri */
            endpoint: string;
            providerModel: string;
            dimensions: number;
        };
        EmbeddingModelCreate: components["schemas"]["EmbeddingModelDescriptor"];
        EmbeddingModelUpdate: {
            name: string;
            /** Format: uri */
            endpoint: string;
            enabled: boolean;
        };
        EmbeddingModel: {
            id: components["schemas"]["UUID"];
            name: string;
            /** Format: uri */
            endpoint: string;
            providerModel: string;
            dimensions: number;
            enabled: boolean;
            /** @enum {string} */
            initializationStatus: "initializing" | "ready" | "failed";
            failureReason: string | null;
            version: number;
            createdAt: components["schemas"]["Timestamp"];
            updatedAt: components["schemas"]["Timestamp"];
        };
        EmbeddingModelList: {
            items: components["schemas"]["EmbeddingModel"][];
        };
        EmbeddingModelOption: {
            model: components["schemas"]["EmbeddingModel"];
            selectable: boolean;
        };
        EmbeddingModelOptionList: {
            items: components["schemas"]["EmbeddingModelOption"][];
        };
        RetrievalStatus: {
            latestTransition?: components["schemas"]["ModelTransition"];
            desiredModelId: components["schemas"]["UUID"];
            servingModelId: components["schemas"]["UUID"] | null;
            transitionJobId: components["schemas"]["UUID"] | null;
        };
        /** @description Verified model-selection operation. Waiting has no admitted Job; retry eligibility is rechecked atomically on admission. */
        ModelTransition: {
            modelId: components["schemas"]["UUID"];
            /** Format: int64 */
            selectionGeneration: number;
            jobId: components["schemas"]["UUID"] | null;
            /** @enum {string} */
            status: "waiting" | "queued" | "running" | "retry_wait" | "succeeded" | "dead" | "cancelled";
            isCurrent: boolean;
            canRetry: boolean;
        };
        GenerationSettings: {
            enabled: boolean;
            endpoint: string | null;
            summaryModel: string | null;
            rerankModel: string | null;
        };
        SummarySettings: {
            language: string;
            maxInputChars: number;
            maxSummaryCharacters: number;
        };
        TaskInstructions: {
            summaryChunk: string;
            summaryReduce: string;
            summaryFinal: string;
            digestRerank: string;
        };
        RetentionSettings: {
            fullContentDays: number | null;
        };
        RetrievalSettings: {
            candidateLimit: number;
            rerankLimit: number;
            rrfK: number;
            recencyHalfLifeDays: number;
            recencyMaxBoost: number;
            feedSourceCap: number;
            digestSourceCap: number;
            digestMmrLambda: number;
            llmRerankEnabled: boolean;
            digestExplorationEnabled: boolean;
        };
        PreferenceProfile: {
            prefer: string;
            avoid: string;
            revision: number;
            version: number;
            updatedAt: components["schemas"]["Timestamp"];
        };
        PreferenceProfilePatch: {
            prefer?: string;
            avoid?: string;
        };
        Source: {
            id: components["schemas"]["UUID"];
            name: string;
            config: components["schemas"]["SourceConfig"];
            enabled: boolean;
            pollIntervalMinutes: number;
            lastSyncedAt: components["schemas"]["Timestamp"] | null;
            createdAt: components["schemas"]["Timestamp"];
            updatedAt: components["schemas"]["Timestamp"];
        };
        Topic: {
            id: components["schemas"]["UUID"];
            name: string;
            sourceIds: components["schemas"]["UUID"][];
            itemKinds: components["schemas"]["ItemKind"][];
            include: string;
            exclude: string;
            mode: components["schemas"]["TopicMode"];
            enabled: boolean;
            revision: number;
            createdAt: components["schemas"]["Timestamp"];
            updatedAt: components["schemas"]["Timestamp"];
        };
        Item: {
            id: components["schemas"]["UUID"];
            sourceIds: components["schemas"]["UUID"][];
            kind: components["schemas"]["ItemKind"];
            details: components["schemas"]["ItemDetails"];
            title: string;
            /** Format: uri */
            url: string;
            /** Format: uri */
            canonicalUrl: string;
            excerpt: string;
            contentMarkdown: string;
            contentOrigin: components["schemas"]["ContentOrigin"];
            /** @description Only proven article body or README has an available generation control. */
            summarySupported: boolean;
            summarySourceVersion: components["schemas"]["SummarySourceVersion"];
            authors: string[];
            publishedAt: components["schemas"]["Timestamp"] | null;
            discoveredAt: components["schemas"]["Timestamp"];
            topicIds: components["schemas"]["UUID"][];
            readingState: components["schemas"]["ReadingState"];
            visibility: components["schemas"]["ItemVisibility"];
            feedback: components["schemas"]["Feedback"] | null;
            images: components["schemas"]["ItemImage"][];
        };
        ItemImage: {
            id: components["schemas"]["UUID"];
            /** Format: uri */
            url: string;
            altText: string | null;
        };
        FeedRefreshInput: {
            topicId?: components["schemas"]["UUID"];
            /** @default 25 */
            limit: number;
        };
        FeedPage: {
            items: components["schemas"]["FeedEntry"][];
            nextCursor: string | null;
            snapshot: string;
            generatedAt: components["schemas"]["Timestamp"];
        };
        DigestDefinition: {
            id: components["schemas"]["UUID"];
            name: string;
            topics: components["schemas"]["DigestTopicInput"][];
            timezone: string;
            recurrence: components["schemas"]["Recurrence"];
            enabled: boolean;
            version: number;
            nextRunAt: components["schemas"]["Timestamp"] | null;
            lastSuccessfulAt: components["schemas"]["Timestamp"] | null;
            createdAt: components["schemas"]["Timestamp"];
            updatedAt: components["schemas"]["Timestamp"];
        };
        /** @enum {string} */
        ContentOrigin: "unknown" | "article_body" | "article_description" | "paper_abstract" | "video_description" | "repository_readme" | "repository_description" | "empty";
        SummarySourceVersion: string;
        /** @description Text and URLs are resolved from owned server content only. */
        ItemSummaryRequest: Record<string, never>;
        ItemSummary: {
            sourceVersion: components["schemas"]["SummarySourceVersion"];
            supported: boolean;
            canGenerate: boolean;
            unavailableReason: ("unsupported_input" | "generation_disabled" | "historical_input") | null;
            /** @enum {string} */
            state: "not_requested" | "pending" | "ready" | "failed";
            jobId: components["schemas"]["UUID"] | null;
            failure: string | null;
            retryable: boolean;
            result: components["schemas"]["AISummary"] | null;
        };
        AISummary: {
            id: components["schemas"]["UUID"];
            generationJobId: components["schemas"]["UUID"];
            sourceVersion: components["schemas"]["SummarySourceVersion"];
            text: string;
            model: string;
            language: string;
            maxInputChars: number;
            maxSummaryCharacters: number;
            instructionsVersion: string;
            algorithmVersion: string;
            createdAt: components["schemas"]["Timestamp"];
        };
        DigestIssueSummary: {
            id: components["schemas"]["UUID"];
            digestId: components["schemas"]["UUID"];
            generationJobId: components["schemas"]["UUID"];
            title: string;
            /** @enum {string} */
            status: "complete" | "degraded";
            scheduledFor: components["schemas"]["Timestamp"] | null;
            generatedAt: components["schemas"]["Timestamp"];
        };
        DigestIssue: {
            id: components["schemas"]["UUID"];
            digestId: components["schemas"]["UUID"];
            generationJobId: components["schemas"]["UUID"];
            title: string;
            /** @enum {string} */
            status: "complete" | "degraded";
            scheduledFor: components["schemas"]["Timestamp"] | null;
            generatedAt: components["schemas"]["Timestamp"];
            sections: components["schemas"]["DigestSection"][];
        };
        Job: {
            modelTransition?: components["schemas"]["ModelTransition"];
            progress?: components["schemas"]["JobProgress"];
            id: components["schemas"]["UUID"];
            kind: string;
            queue: components["schemas"]["JobQueue"];
            /** @enum {string} */
            status: "queued" | "running" | "retry_wait" | "succeeded" | "dead" | "cancelled";
            attempt: number;
            maxAttempts: number;
            cancellationRequested: boolean;
            reason?: string | null;
            createdAt: components["schemas"]["Timestamp"];
            startedAt?: components["schemas"]["Timestamp"] | null;
            finishedAt?: components["schemas"]["Timestamp"] | null;
            /** Format: int64 */
            durationMs?: number | null;
            updatedAt: components["schemas"]["Timestamp"];
        };
        /** @description Fixed initial Item cohort progress. Equal counts do not imply successful catch-up or cutover; Job status remains authoritative. */
        JobProgress: {
            /** @enum {string} */
            phase: "preparing" | "indexing" | "catching_up" | "activating" | "completed";
            /** Format: int64 */
            processedItems: number;
            /**
             * Format: int64
             * @description Fixed initial cohort size, unknown until preparation finishes. New Items do not increase this total.
             */
            totalItems: number | null;
            updatedAt: components["schemas"]["Timestamp"];
        };
        /** @enum {string} */
        JobQueue: "fetch" | "generation" | "retrieval" | "feed-preparation" | "maintenance";
        /** Format: uuid */
        UUID: string;
        ComponentStatus: {
            name: string;
            /** @enum {string} */
            status: "ready" | "degraded" | "unavailable";
            detail?: string;
        };
        /** Format: date-time */
        Timestamp: string;
        SourceConfig: components["schemas"]["RssSourceConfig"] | components["schemas"]["AtomSourceConfig"] | components["schemas"]["ArxivSourceConfig"] | components["schemas"]["GithubTrendingSourceConfig"] | components["schemas"]["YoutubeSourceConfig"];
        RssSourceConfig: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            kind: "rss";
            /** Format: uri */
            url: string;
            /** @default false */
            allowPrivateNetwork: boolean;
        };
        AtomSourceConfig: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            kind: "atom";
            /** Format: uri */
            url: string;
            /** @default false */
            allowPrivateNetwork: boolean;
        };
        ArxivSourceConfig: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            kind: "arxiv";
            /** @example cat:cs.AI */
            query: string;
        };
        GithubTrendingSourceConfig: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            kind: "github";
            /** @enum {string} */
            period: "daily" | "weekly" | "monthly";
            /** @enum {string} */
            language: "all" | "python" | "rust" | "go" | "javascript" | "typescript";
        };
        YoutubeSourceConfig: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            kind: "youtube";
            channelId: string;
        };
        /** @enum {string} */
        ItemKind: "article" | "paper" | "video" | "repository";
        ItemDetails: components["schemas"]["ArticleDetails"] | components["schemas"]["PaperDetails"] | components["schemas"]["VideoDetails"] | components["schemas"]["RepositoryDetails"];
        ArticleDetails: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            type: "article";
            description: string;
        };
        PaperDetails: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            type: "paper";
            abstract: string;
            categories: string[];
            doi: string | null;
        };
        VideoDetails: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            type: "video";
            description: string;
            channelId: string;
            videoId: string;
        };
        RepositoryDetails: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            type: "repository";
            description: string | null;
            homepage: string | null;
            programmingLanguage: string | null;
            stars: number | null;
            forks: number | null;
            openIssues: number | null;
            topics: string[];
            defaultBranch: string | null;
            starsInPeriod: number | null;
            rank: number;
            /** @enum {string} */
            period: "daily" | "weekly" | "monthly";
            observedAt: components["schemas"]["Timestamp"];
            /** @enum {string} */
            snapshotStatus: "complete" | "degraded";
        };
        /** @enum {string} */
        TopicMode: "ordinary" | "strict";
        /**
         * @description Preserve current membership corrections by default, or clear the complete set explicitly.
         * @enum {string}
         */
        TopicCorrectionPolicy: "preserve" | "clear";
        SourceList: {
            items: components["schemas"]["Source"][];
        };
        SourceInput: {
            name: string;
            config: components["schemas"]["SourceConfig"];
            enabled: boolean;
            /** @default 60 */
            pollIntervalMinutes: number;
        };
        JobAccepted: {
            jobId: components["schemas"]["UUID"];
        };
        SourcePatch: {
            name?: string;
            config?: components["schemas"]["SourceConfig"];
            enabled?: boolean;
            pollIntervalMinutes?: number;
        };
        TopicList: {
            items: components["schemas"]["Topic"][];
        };
        TopicInput: {
            name: string;
            sourceIds: components["schemas"]["UUID"][];
            itemKinds: components["schemas"]["ItemKind"][];
            include: string;
            exclude: string;
            mode: components["schemas"]["TopicMode"];
            enabled: boolean;
        };
        TopicPatch: {
            name?: string;
            sourceIds?: components["schemas"]["UUID"][];
            itemKinds?: components["schemas"]["ItemKind"][];
            include?: string;
            exclude?: string;
            mode?: components["schemas"]["TopicMode"];
            enabled?: boolean;
            correctionPolicy?: components["schemas"]["TopicCorrectionPolicy"];
        };
        /** @enum {string} */
        ReadingState: "unread" | "read";
        /** @enum {string} */
        ItemVisibility: "visible" | "hidden";
        Feedback: {
            /** @enum {string} */
            sentiment: "like" | "dislike";
            comment: string | null;
            createdAt: components["schemas"]["Timestamp"];
            updatedAt: components["schemas"]["Timestamp"];
        };
        FeedExplanation: {
            summary: string;
            signals: {
                [key: string]: number;
            };
            pipelineVersion: string;
        };
        FeedEntry: {
            item: components["schemas"]["Item"];
            score: number;
            explanation: components["schemas"]["FeedExplanation"];
        };
        SearchPage: {
            items: components["schemas"]["FeedEntry"][];
            nextCursor: string | null;
        };
        FeedbackInput: {
            /** @enum {string} */
            sentiment: "like" | "dislike";
            comment: string | null;
        };
        ReadingStateInput: {
            state: components["schemas"]["ReadingState"];
        };
        VisibilityInput: {
            visibility: components["schemas"]["ItemVisibility"];
        };
        TopicCorrectionInput: {
            topicId: components["schemas"]["UUID"];
            /** @enum {string} */
            membership: "include" | "exclude" | "automatic";
        };
        Recurrence: components["schemas"]["DailyRecurrence"] | components["schemas"]["WeeklyRecurrence"] | components["schemas"]["MonthlyRecurrence"];
        DailyRecurrence: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            frequency: "daily";
            hour: number;
            minute: number;
        };
        WeeklyRecurrence: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            frequency: "weekly";
            weekdays: number[];
            hour: number;
            minute: number;
        };
        MonthlyRecurrence: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            frequency: "monthly";
            rule: components["schemas"]["MonthlyDayOfMonthRule"] | components["schemas"]["MonthlyNthWeekdayRule"] | components["schemas"]["MonthlyLastDayRule"];
            hour: number;
            minute: number;
        };
        MonthlyDayOfMonthRule: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            type: "dayOfMonth";
            day: number;
        };
        MonthlyNthWeekdayRule: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            type: "nthWeekday";
            weekday: number;
            occurrence: number;
        };
        MonthlyLastDayRule: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            type: "lastDay";
        };
        DigestTopicInput: {
            topicId: components["schemas"]["UUID"];
            /** @default 5 */
            itemLimit: number;
        };
        DigestList: {
            items: components["schemas"]["DigestDefinition"][];
        };
        DigestInput: {
            name: string;
            topics: components["schemas"]["DigestTopicInput"][];
            timezone: string;
            recurrence: components["schemas"]["Recurrence"];
            enabled: boolean;
        };
        DigestPatch: {
            name?: string;
            topics?: components["schemas"]["DigestTopicInput"][];
            timezone?: string;
            recurrence?: components["schemas"]["Recurrence"];
            enabled?: boolean;
        };
        DigestSection: {
            topicId: components["schemas"]["UUID"];
            topicName: string;
            topicRevision: number;
            items: components["schemas"]["DigestItem"][];
            failure: string | null;
        };
        DigestItem: {
            item: components["schemas"]["Item"];
            summary: string;
            explanation: components["schemas"]["FeedExplanation"];
            /** @description Separate annotation state for the exact saved Item version. Reading never generates text. */
            aiSummary: components["schemas"]["ItemSummary"] | null;
        };
        DigestIssueList: {
            items: components["schemas"]["DigestIssueSummary"][];
            nextCursor: string | null;
        };
        JobList: {
            items: components["schemas"]["Job"][];
            nextCursor: string | null;
        };
        ExportBundle: {
            /** @constant */
            formatVersion: "topic-stream/v2";
            exportedAt: components["schemas"]["Timestamp"];
            workspaceId: components["schemas"]["UUID"];
            settings: components["schemas"]["WorkspaceSettings"];
            embeddingModel: components["schemas"]["EmbeddingModelDescriptor"];
            preferenceProfile: components["schemas"]["PreferenceProfile"];
            sources: components["schemas"]["Source"][];
            topics: components["schemas"]["Topic"][];
            feedback: components["schemas"]["PortableFeedback"][];
            digests: components["schemas"]["DigestDefinition"][];
        };
        PortableFeedback: {
            /** Format: uri */
            canonicalUrl: string;
            feedback: components["schemas"]["Feedback"];
        };
    };
    responses: {
        /** @description Request failed */
        problem: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/problem+json": components["schemas"]["Problem"];
            };
        };
        /** @description Canonical PostgreSQL state committed but its retrieval projection could not be delivered */
        "projection-pending": {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/problem+json": components["schemas"]["ProjectionPendingProblem"];
            };
        };
    };
    parameters: {
        /** @description Quoted integer version returned in the resource ETag. */
        "if-match-version": string;
        "source-id": string;
        "topic-id": string;
        "item-id": string;
        "image-id": string;
        "digest-id": string;
        "issue-id": string;
        "job-id": string;
        "model-id": string;
    };
    requestBodies: never;
    headers: {
        /** @description Quoted integer resource version for optimistic updates. */
        "version-etag": string;
    };
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    getSystemStatus: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current daemon and dependency status */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SystemStatus"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    streamEvents: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Server-sent event stream; clients must recover by refetching HTTP resources */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "text/event-stream": string;
                };
            };
            default: components["responses"]["problem"];
        };
    };
    listEmbeddingModels: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Registered embedding models */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EmbeddingModelList"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    createEmbeddingModel: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["EmbeddingModelCreate"];
            };
        };
        responses: {
            /** @description Registered model, including a failed initialization state when initialization could not complete */
            201: {
                headers: {
                    ETag: components["headers"]["version-etag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EmbeddingModel"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    getEmbeddingModel: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                modelId: components["parameters"]["model-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Registered embedding model */
            200: {
                headers: {
                    ETag: components["headers"]["version-etag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EmbeddingModel"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    updateEmbeddingModel: {
        parameters: {
            query?: never;
            header: {
                /** @description Quoted integer version returned in the resource ETag. */
                "If-Match": components["parameters"]["if-match-version"];
            };
            path: {
                modelId: components["parameters"]["model-id"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["EmbeddingModelUpdate"];
            };
        };
        responses: {
            /** @description Updated embedding model */
            200: {
                headers: {
                    ETag: components["headers"]["version-etag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EmbeddingModel"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    initializeEmbeddingModel: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                modelId: components["parameters"]["model-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Embedding model after the initialization attempt */
            200: {
                headers: {
                    ETag: components["headers"]["version-etag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EmbeddingModel"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    listEmbeddingModelOptions: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Selectable models and current desired or serving models */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EmbeddingModelOptionList"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    getRetrievalStatus: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current workspace retrieval state */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RetrievalStatus"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    getWorkspaceSettings: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current workspace settings */
            200: {
                headers: {
                    ETag: components["headers"]["version-etag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceSettings"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    updateWorkspaceSettings: {
        parameters: {
            query?: never;
            header: {
                /** @description Quoted integer version returned in the resource ETag. */
                "If-Match": components["parameters"]["if-match-version"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["WorkspaceSettingsPatch"];
            };
        };
        responses: {
            /** @description Updated workspace settings */
            200: {
                headers: {
                    ETag: components["headers"]["version-etag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceSettings"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    getPreferenceProfile: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current preference profile */
            200: {
                headers: {
                    ETag: components["headers"]["version-etag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PreferenceProfile"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    updatePreferenceProfile: {
        parameters: {
            query?: never;
            header: {
                /** @description Quoted integer version returned in the resource ETag. */
                "If-Match": components["parameters"]["if-match-version"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PreferenceProfilePatch"];
            };
        };
        responses: {
            /** @description Updated preference profile */
            200: {
                headers: {
                    ETag: components["headers"]["version-etag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PreferenceProfile"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    listSources: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Configured sources */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SourceList"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    createSource: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SourceInput"];
            };
        };
        responses: {
            /** @description Source created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Source"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    syncAllSources: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Synchronization job accepted */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["JobAccepted"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    getSource: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                sourceId: components["parameters"]["source-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Source */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Source"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    deleteSource: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                sourceId: components["parameters"]["source-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Source deleted */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["problem"];
        };
    };
    updateSource: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                sourceId: components["parameters"]["source-id"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SourcePatch"];
            };
        };
        responses: {
            /** @description Updated source */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Source"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    syncSource: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                sourceId: components["parameters"]["source-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Synchronization job accepted */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["JobAccepted"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    listTopics: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Configured topics */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TopicList"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    createTopic: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["TopicInput"];
            };
        };
        responses: {
            /** @description Topic created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Topic"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    deleteTopic: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                topicId: components["parameters"]["topic-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Topic deleted */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["problem"];
        };
    };
    updateTopic: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                topicId: components["parameters"]["topic-id"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["TopicPatch"];
            };
        };
        responses: {
            /** @description Updated topic */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Topic"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    getFeed: {
        parameters: {
            query?: {
                limit?: number;
                /** @description Opaque cursor for older items within the same snapshot */
                cursor?: string;
                /** @description Snapshot token returned by the first page; omit it to read the latest saved head without publishing */
                snapshot?: string;
                topicId?: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Saved Feed page */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FeedPage"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    refreshFeed: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["FeedRefreshInput"];
            };
        };
        responses: {
            /** @description Latest saved Feed page, including when no new batch was published */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FeedPage"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    searchItems: {
        parameters: {
            query?: {
                q?: string;
                mode?: "lexical" | "semantic" | "hybrid";
                limit?: number;
                cursor?: string;
                topicId?: string;
                sourceId?: string;
                publishedAfter?: string;
                publishedBefore?: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Search results */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SearchPage"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    getItem: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Canonical item and personal state */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Item"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    getItemSummary: {
        parameters: {
            query?: {
                /** @description Read only the exact saved input version for Snapshot capture. */
                sourceVersion?: components["schemas"]["SummarySourceVersion"];
            };
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Input support, generation permission and existing annotation state */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ItemSummary"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    requestItemSummary: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ItemSummaryRequest"];
            };
        };
        responses: {
            /** @description Accepted active work or an already cached result */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["JobAccepted"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    requestDigestItemSummary: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                issueId: components["parameters"]["issue-id"];
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ItemSummaryRequest"];
            };
        };
        responses: {
            /** @description Accepted shared work or an immutable ready annotation */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["JobAccepted"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    setItemFeedback: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["FeedbackInput"];
            };
        };
        responses: {
            /** @description Current feedback */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Feedback"];
                };
            };
            503: components["responses"]["projection-pending"];
            default: components["responses"]["problem"];
        };
    };
    clearItemFeedback: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Feedback removed */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            503: components["responses"]["projection-pending"];
            default: components["responses"]["problem"];
        };
    };
    setItemReadingState: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ReadingStateInput"];
            };
        };
        responses: {
            /** @description Reading state updated */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["problem"];
        };
    };
    setItemVisibility: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["VisibilityInput"];
            };
        };
        responses: {
            /** @description Visibility updated */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["problem"];
        };
    };
    setItemTopicCorrection: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["TopicCorrectionInput"];
            };
        };
        responses: {
            /** @description Topic membership correction updated */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            503: components["responses"]["projection-pending"];
            default: components["responses"]["problem"];
        };
    };
    downloadItemImage: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                itemId: components["parameters"]["item-id"];
                imageId: components["parameters"]["image-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Validated image bytes */
            200: {
                headers: {
                    "X-Content-Type-Options": "nosniff";
                    [name: string]: unknown;
                };
                content: {
                    "image/*": string;
                };
            };
            default: components["responses"]["problem"];
        };
    };
    listDigests: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Digest definitions */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DigestList"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    createDigest: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["DigestInput"];
            };
        };
        responses: {
            /** @description Digest definition created */
            201: {
                headers: {
                    ETag: components["headers"]["version-etag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DigestDefinition"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    deleteDigest: {
        parameters: {
            query?: never;
            header: {
                /** @description Quoted integer version returned in the resource ETag. */
                "If-Match": components["parameters"]["if-match-version"];
            };
            path: {
                digestId: components["parameters"]["digest-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Digest definition deleted */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["problem"];
        };
    };
    updateDigest: {
        parameters: {
            query?: never;
            header: {
                /** @description Quoted integer version returned in the resource ETag. */
                "If-Match": components["parameters"]["if-match-version"];
            };
            path: {
                digestId: components["parameters"]["digest-id"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["DigestPatch"];
            };
        };
        responses: {
            /** @description Digest definition updated */
            200: {
                headers: {
                    ETag: components["headers"]["version-etag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DigestDefinition"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    generateDigest: {
        parameters: {
            query?: never;
            header: {
                /** @description Permanent request identity scoped to this Workspace and Digest Definition. */
                "Idempotency-Key": string;
            };
            path: {
                digestId: components["parameters"]["digest-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Generation job accepted */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["JobAccepted"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    listDigestIssues: {
        parameters: {
            query?: {
                digestId?: string;
                /** @description Maximum number of summaries per page. */
                limit?: number;
                /** @description Opaque continuation bound to the current Digest filter. */
                cursor?: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Digest Issue summaries ordered by generatedAt and id descending */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DigestIssueList"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    getDigestIssue: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                issueId: components["parameters"]["issue-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Immutable digest issue */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DigestIssue"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    listJobs: {
        parameters: {
            query?: {
                status?: "queued" | "running" | "retry_wait" | "succeeded" | "dead" | "cancelled";
                limit?: number;
                cursor?: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Jobs ordered from newest to oldest */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["JobList"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    getJob: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                jobId: components["parameters"]["job-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Job details */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Job"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    retryJob: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                jobId: components["parameters"]["job-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Replacement job accepted */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["JobAccepted"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    cancelJob: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                jobId: components["parameters"]["job-id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Cancellation accepted */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Job"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    exportWorkspace: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Portable workspace configuration and personal state */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ExportBundle"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
    importWorkspace: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ExportBundle"];
            };
        };
        responses: {
            /** @description Import job accepted */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["JobAccepted"];
                };
            };
            default: components["responses"]["problem"];
        };
    };
}
