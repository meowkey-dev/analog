# CORRESPONDENCE — python harness to go suite

Every python test in `tests/contract/` and its go counterpart in
`tests/conformance-go/`. One row per python test function; parametrized cases
become go subtests. `TestParity_*` in the go suite enforces operation and
fixture coverage on **both** suites while the python one exists (issue #58).

Phase 2 deletes the python column along with `tests/`, `pytest.ini` and the uv
CI steps.

¹ The python check scans imports; the go check inspects the module's go.mod
  and its whole test-binary dependency graph — a stronger form of the same rule.
² The full OpenAPI 3.1 metaschema validation (openapi_spec_validator) has no go
  counterpart and retires with the python suite; the structural assertions the
  suite relies on are ported.

## `test_actor.py` → `actor_test.go`

| python test | go test |
|---|---|
| `test_mutations_reject_a_missing_actor` | `TestActor_MutationsRejectAMissingActor` |
| `test_mutations_reject_an_unknown_actor_kind` | `TestActor_MutationsRejectAnUnknownActorKind` |
| `test_media_upload_requires_an_actor` | `TestActor_MediaUploadRequiresAnActor` |
| `test_feedback_requires_an_actor` | `TestActor_FeedbackRequiresAnActor` |
| `test_reads_do_not_require_an_actor` | `TestActor_ReadsDoNotRequireAnActor` |
| `test_actor_is_recorded_on_what_it_writes` | `TestActor_ActorIsRecordedOnWhatItWrites` |

## `test_annotations.py` → `annotations_test.go`

| python test | go test |
|---|---|
| `test_all_v1_selectors_round_trip` | `TestAnnotations_AllV1SelectorsRoundTrip` |
| `test_selector_defaults_to_the_whole_card` | `TestAnnotations_SelectorDefaultsToTheWholeCard` |
| `test_motivation_defaults_to_commenting` | `TestAnnotations_MotivationDefaultsToCommenting` |
| `test_motivations` | `TestAnnotations_Motivations` |
| `test_unknown_motivation_is_rejected` | `TestAnnotations_UnknownMotivationIsRejected` |
| `test_creation_records_the_creator_and_the_cards_rev` | `TestAnnotations_CreationRecordsTheCreatorAndTheCardsRev` |
| `test_agents_can_annotate_too` | `TestAnnotations_AgentsCanAnnotateToo` |
| `test_annotating_an_unknown_card_is_404` | `TestAnnotations_AnnotatingAnUnknownCardIs404` |
| `test_an_edit_makes_the_annotation_stale` | `TestAnnotations_AnEditMakesTheAnnotationStale` |
| `test_a_move_does_not_make_the_annotation_stale` | `TestAnnotations_AMoveDoesNotMakeTheAnnotationStale` |
| `test_stale_annotations_are_not_auto_resolved` | `TestAnnotations_StaleAnnotationsAreNotAutoResolved` |
| `test_resolve_with_a_reply` | `TestAnnotations_ResolveWithAReply` |
| `test_resolved_defaults_to_true` | `TestAnnotations_ResolvedDefaultsToTrue` |
| `test_reopening_emits_no_resolve_event` | `TestAnnotations_ReopeningEmitsNoResolveEvent` |
| `test_resolving_an_unknown_annotation_is_404` | `TestAnnotations_ResolvingAnUnknownAnnotationIs404` |
| `test_list_order_is_creation_order` | `TestAnnotations_ListOrderIsCreationOrder` |

## `test_auth.py` → `auth_test.go`

| python test | go test |
|---|---|
| `test_health_on_an_open_server` | `TestAuth_HealthOnAnOpenServer` |
| `test_health_needs_no_token_and_says_one_is_needed` | `TestAuth_HealthNeedsNoTokenAndSaysOneIsNeeded` |
| `test_reads_require_a_token_once_one_exists` | `TestAuth_ReadsRequireATokenOnceOneExists` |
| `test_writes_require_a_token` | `TestAuth_WritesRequireAToken` |
| `test_a_bad_token_is_401` | `TestAuth_ABadTokenIs401` |
| `test_a_valid_token_gets_through` | `TestAuth_AValidTokenGetsThrough` |
| `test_whoami_reports_the_token_identity` | `TestAuth_WhoamiReportsTheTokenIdentity` |
| `test_whoami_on_an_open_server` | `TestAuth_WhoamiOnAnOpenServer` |
| `test_the_token_decides_who_you_are` | `TestAuth_TheTokenDecidesWhoYouAre` |
| `test_a_matching_claim_is_accepted_and_attributed` | `TestAuth_AMatchingClaimIsAcceptedAndAttributed` |
| `test_the_actor_params_are_still_required` | `TestAuth_TheActorParamsAreStillRequired` |
| `test_kind_must_match_too` | `TestAuth_KindMustMatchToo` |
| `test_media_is_not_readable_without_a_token` | `TestAuth_MediaIsNotReadableWithoutAToken` |
| `test_the_event_stream_needs_a_token` | `TestAuth_TheEventStreamNeedsAToken` |
| `test_a_cors_preflight_is_never_gated` | `TestAuth_ACorsPreflightIsNeverGated` |
| `test_a_401_still_carries_cors_headers` | `TestAuth_A401StillCarriesCorsHeaders` |
| `test_the_tauri_origin_is_allowed_by_default` | `TestAuth_TheTauriOriginIsAllowedByDefault` |
| `test_loopback_origins_are_allowed_by_default` | `TestAuth_LoopbackOriginsAreAllowedByDefault` |
| `test_an_open_server_is_unchanged` | `TestAuth_AnOpenServerIsUnchanged` |
| `test_the_contract_documents_bearer_auth` | `TestAuth_TheContractDocumentsBearerAuth` |
| `test_the_contract_documents_health_as_public` | `TestAuth_TheContractDocumentsHealthAsPublic` |
| `test_the_contract_documents_whoami` | `TestAuth_TheContractDocumentsWhoami` |
| `test_the_error_enum_covers_the_new_codes` | `TestAuth_TheErrorEnumCoversTheNewCodes` |

## `test_black_box.py` → `black_box_test.go`

| python test | go test |
|---|---|
| `test_no_contract_test_imports_the_implementation` | `TestBlackBox_NoAnalogModule` + `TestBlackBox_NoAnalogPackagesInTestBinary` ¹ |

## `test_cards.py` → `cards_test.go`

| python test | go test |
|---|---|
| `test_drafts_become_json_canvas_text_nodes` | `TestCards_DraftsBecomeJsonCanvasTextNodes` |
| `test_kind_defaults_to_md` | `TestCards_KindDefaultsToMd` |
| `test_all_kinds_accepted` | `TestCards_AllKindsAccepted` |
| `test_unknown_kind_is_rejected` | `TestCards_UnknownKindIsRejected` |
| `test_bulk_create_returns_nodes_in_request_order` | `TestCards_BulkCreateReturnsNodesInRequestOrder` |
| `test_meta_is_stored_verbatim` | `TestCards_MetaIsStoredVerbatim` |
| `test_raw_nodes_are_accepted_and_reattributed` | `TestCards_RawNodesAreAcceptedAndReattributed` |
| `test_creating_in_an_unknown_space_is_404` | `TestCards_CreatingInAnUnknownSpaceIs404` |
| `test_first_card_lands_at_the_origin` | `TestCards_FirstCardLandsAtTheOrigin` |
| `test_omitted_geometry_goes_right_of_the_bounding_box_top_down` | `TestCards_OmittedGeometryGoesRightOfTheBoundingBoxTopDown` |
| `test_explicit_geometry_wins` | `TestCards_ExplicitGeometryWins` |
| `test_layout_ignores_deleted_cards` | `TestCards_LayoutIgnoresDeletedCards` |
| `test_geometry_only_patch_moves_without_bumping_rev` | `TestCards_GeometryOnlyPatchMovesWithoutBumpingRev` |
| `test_resize_is_a_move_not_an_edit` | `TestCards_ResizeIsAMoveNotAnEdit` |
| `test_a_no_op_move_still_emits_one_move_event` | `TestCards_ANoOpMoveStillEmitsOneMoveEvent` |
| `test_content_patch_updates_and_bumps_rev` | `TestCards_ContentPatchUpdatesAndBumpsRev` |
| `test_non_geometry_patches_are_edits` | `TestCards_NonGeometryPatchesAreEdits` |
| `test_mixed_patch_is_an_edit` | `TestCards_MixedPatchIsAnEdit` |
| `test_patch_preserves_unmentioned_keys` | `TestCards_PatchPreservesUnmentionedKeys` |
| `test_patch_cannot_change_the_id` | `TestCards_PatchCannotChangeTheId` |
| `test_empty_patch_is_rejected` | `TestCards_EmptyPatchIsRejected` |
| `test_patching_an_unknown_card_is_404` | `TestCards_PatchingAnUnknownCardIs404` |
| `test_if_match_on_the_current_rev_succeeds` | `TestCards_IfMatchOnTheCurrentRevSucceeds` |
| `test_if_match_mismatch_is_409_with_the_current_node` | `TestCards_IfMatchMismatchIs409WithTheCurrentNode` |
| `test_absent_if_match_is_last_write_wins` | `TestCards_AbsentIfMatchIsLastWriteWins` |
| `test_delete_is_soft` | `TestCards_DeleteIsSoft` |
| `test_deleting_twice_is_404` | `TestCards_DeletingTwiceIs404` |
| `test_patching_a_deleted_card_is_404` | `TestCards_PatchingADeletedCardIs404` |
| `test_deleting_an_unknown_card_is_404` | `TestCards_DeletingAnUnknownCardIs404` |
| `test_a_long_batch_wraps_into_a_new_column` | `TestCards_ALongBatchWrapsIntoANewColumn` |
| `test_wrapping_columns_do_not_overlap` | `TestCards_WrappingColumnsDoNotOverlap` |

## `test_events.py` → `events_test.go`

| python test | go test |
|---|---|
| `test_each_mutation_emits_exactly_one_event` | `TestEvents_EachMutationEmitsExactlyOneEvent` |
| `test_bulk_create_emits_one_event_per_item` | `TestEvents_BulkCreateEmitsOneEventPerItem` |
| `test_a_failed_mutation_emits_nothing` | `TestEvents_AFailedMutationEmitsNothing` |
| `test_events_validate_and_carry_attribution` | `TestEvents_EventsValidateAndCarryAttribution` |
| `test_seq_starts_at_one_and_is_contiguous` | `TestEvents_SeqStartsAtOneAndIsContiguous` |
| `test_subject_id_points_at_the_thing_that_changed` | `TestEvents_SubjectIdPointsAtTheThingThatChanged` |
| `test_card_created_payload_carries_the_title` | `TestEvents_CardCreatedPayloadCarriesTheTitle` |
| `test_link_created_payload_carries_endpoints_and_label` | `TestEvents_LinkCreatedPayloadCarriesEndpointsAndLabel` |
| `test_since_is_exclusive` | `TestEvents_SinceIsExclusive` |
| `test_limit_and_cursor` | `TestEvents_LimitAndCursor` |
| `test_cursor_when_nothing_is_returned` | `TestEvents_CursorWhenNothingIsReturned` |
| `test_events_for_an_unknown_space_is_404` | `TestEvents_EventsForAnUnknownSpaceIs404` |
| `test_stream_replays_the_backlog_then_pushes_live_events` | `TestEvents_StreamReplaysTheBacklogThenPushesLiveEvents` |
| `test_stream_resumes_from_last_event_id` | `TestEvents_StreamResumesFromLastEventID` |

## `test_feedback.py` → `feedback_test.go`

| python test | go test |
|---|---|
| `test_an_agent_never_reads_its_own_writes_back` | `TestFeedback_AnAgentNeverReadsItsOwnWritesBack` |
| `test_another_agents_writes_are_feedback` | `TestFeedback_AnotherAgentsWritesAreFeedback` |
| `test_cursors_are_independent_per_actor` | `TestFeedback_CursorsAreIndependentPerActor` |
| `test_unresolved_annotations_come_back_every_call` | `TestFeedback_UnresolvedAnnotationsComeBackEveryCall` |
| `test_resolved_annotations_disappear` | `TestFeedback_ResolvedAnnotationsDisappear` |
| `test_an_agent_sees_its_own_annotations_too` | `TestFeedback_AnAgentSeesItsOwnAnnotationsToo` |
| `test_annotations_carry_card_title_and_staleness` | `TestFeedback_AnnotationsCarryCardTitleAndStaleness` |
| `test_annotations_on_deleted_cards_still_surface` | `TestFeedback_AnnotationsOnDeletedCardsStillSurface` |
| `test_a_reply_on_resolve_reaches_the_other_side_once` | `TestFeedback_AReplyOnResolveReachesTheOtherSideOnce` |
| `test_nobody_reads_their_own_reply_back` | `TestFeedback_NobodyReadsTheirOwnReplyBack` |
| `test_resolving_without_a_reply_is_still_pure_acknowledgment` | `TestFeedback_ResolvingWithoutAReplyIsStillPureAcknowledgment` |
| `test_reopening_and_resolving_again_delivers_again` | `TestFeedback_ReopeningAndResolvingAgainDeliversAgain` |
| `test_a_fresh_actor_starts_at_zero` | `TestFeedback_AFreshActorStartsAtZero` |
| `test_advance_false_does_not_consume` | `TestFeedback_AdvanceFalseDoesNotConsume` |
| `test_explicit_since_overrides_the_stored_cursor` | `TestFeedback_ExplicitSinceOverridesTheStoredCursor` |
| `test_cursor_is_always_the_spaces_current_seq` | `TestFeedback_CursorIsAlwaysTheSpacesCurrentSeq` |
| `test_feedback_on_an_unknown_space_is_404` | `TestFeedback_FeedbackOnAnUnknownSpaceIs404` |
| `test_moves_are_bucketed_away_from_edits` | `TestFeedback_MovesAreBucketedAwayFromEdits` |
| `test_repeated_events_on_one_card_collapse_to_one_row` | `TestFeedback_RepeatedEventsOnOneCardCollapseToOneRow` |
| `test_changed_keys_are_unioned_across_edits` | `TestFeedback_ChangedKeysAreUnionedAcrossEdits` |
| `test_a_deletion_supersedes_an_edit_or_a_move` | `TestFeedback_ADeletionSupersedesAnEditOrAMove` |
| `test_an_edit_supersedes_a_move` | `TestFeedback_AnEditSupersedesAMove` |
| `test_a_link_created_and_removed_in_the_window_reports_as_neither` | `TestFeedback_ALinkCreatedAndRemovedInTheWindowReportsAsNeither` |
| `test_links_report_endpoints_and_label` | `TestFeedback_LinksReportEndpointsAndLabel` |
| `test_link_removal_is_reported` | `TestFeedback_LinkRemovalIsReported` |
| `test_summary_is_empty_when_nothing_changed` | `TestFeedback_SummaryIsEmptyWhenNothingChanged` |
| `test_summary_singular_and_plural` | `TestFeedback_SummarySingularAndPlural` |
| `test_summary_counts_stale` | `TestFeedback_SummaryCountsStale` |
| `test_summary_reproduces_the_fixture_grammar` | `TestFeedback_SummaryReproducesTheFixtureGrammar` |
| `test_summary_reports_removed_links` | `TestFeedback_SummaryReportsRemovedLinks` |
| `test_summary_slots_replies_after_comments` | `TestFeedback_SummarySlotsRepliesAfterComments` |

## `test_fixtures.py` → `fixtures_test.go`

| python test | go test |
|---|---|
| `test_space_matches_schema` | `TestFixtures_SpaceMatchesSchema` |
| `test_canvas_matches_schema` | `TestFixtures_CanvasMatchesSchema` |
| `test_canvas_is_valid_json_canvas_10` | `TestFixtures_CanvasIsValidJsonCanvas10` |
| `test_annotations_match_schema` | `TestFixtures_AnnotationsMatchSchema` |
| `test_events_match_schema` | `TestFixtures_EventsMatchSchema` |
| `test_feedback_matches_schema` | `TestFixtures_FeedbackMatchesSchema` |
| `test_fixture_inventory` | `TestFixtures_FixtureInventory` |
| `test_event_seqs_are_contiguous_from_one` | `TestFixtures_EventSeqsAreContiguousFromOne` |
| `test_every_event_subject_exists` | `TestFixtures_EveryEventSubjectExists` |
| `test_own_events_are_filtered_from_the_authors_feedback` | `TestFixtures_OwnEventsAreFilteredFromTheAuthorsFeedback` |
| `test_unresolved_annotations_ignore_the_cursor` | `TestFixtures_UnresolvedAnnotationsIgnoreTheCursor` |
| `test_a_reply_on_resolve_reaches_the_other_actor_once` | `TestFixtures_AReplyOnResolveReachesTheOtherActorOnce` |
| `test_resolved_annotations_are_excluded` | `TestFixtures_ResolvedAnnotationsAreExcluded` |
| `test_staleness_is_card_rev_less_than_current_rev` | `TestFixtures_StalenessIsCardRevLessThanCurrentRev` |
| `test_moved_is_not_edited` | `TestFixtures_MovedIsNotEdited` |
| `test_soft_delete_keeps_the_card_visible_to_agents` | `TestFixtures_SoftDeleteKeepsTheCardVisibleToAgents` |
| `test_all_four_render_paths_are_present` | `TestFixtures_AllFourRenderPathsArePresent` |
| `test_html_card_carries_a_script_for_the_sandbox_test` | `TestFixtures_HtmlCardCarriesAScriptForTheSandboxTest` |
| `test_selectors_cover_both_v1_shapes` | `TestFixtures_SelectorsCoverBothV1Shapes` |
| `test_deltas_agree_with_the_event_log_after_seq_12` | `TestFixtures_DeltasAgreeWithTheEventLogAfterSeq12` |

## `test_fixtures_roundtrip.py` → `roundtrip_test.go`

| python test | go test |
|---|---|
| `test_space_matches_fixture` | `TestRoundtrip_SpaceMatchesFixture` |
| `test_space_list_contains_only_the_seeded_space` | `TestRoundtrip_SpaceListContainsOnlyTheSeededSpace` |
| `test_canvas_matches_fixture` | `TestRoundtrip_CanvasMatchesFixture` |
| `test_canvas_include_deleted_matches_fixture` | `TestRoundtrip_CanvasIncludeDeletedMatchesFixture` |
| `test_live_canvas_never_leaks_the_tombstone` | `TestRoundtrip_LiveCanvasNeverLeaksTheTombstone` |
| `test_annotations_match_fixture` | `TestRoundtrip_AnnotationsMatchFixture` |
| `test_annotation_filters` | `TestRoundtrip_AnnotationFilters` |
| `test_events_match_fixture` | `TestRoundtrip_EventsMatchFixture` |
| `test_feedback_matches_fixture_without_an_explicit_since` | `TestRoundtrip_FeedbackMatchesFixtureWithoutAnExplicitSince` |
| `test_feedback_matches_fixture_with_an_explicit_since` | `TestRoundtrip_FeedbackMatchesFixtureWithAnExplicitSince` |
| `test_feedback_matches_the_human_fixture` | `TestRoundtrip_FeedbackMatchesTheHumanFixture` |
| `test_advance_consumes_the_cursor` | `TestRoundtrip_AdvanceConsumesTheCursor` |
| `test_peeking_does_not_consume` | `TestRoundtrip_PeekingDoesNotConsume` |
| `test_an_unknown_actor_starts_at_zero_and_sees_everything` | `TestRoundtrip_AnUnknownActorStartsAtZeroAndSeesEverything` |
| `test_the_media_referenced_by_the_file_node_is_served` | `TestRoundtrip_TheMediaReferencedByTheFileNodeIsServed` |
| `test_seeded_responses_validate` | `TestRoundtrip_SeededResponsesValidate` |

## `test_import.py` → `import_test.go`

| python test | go test |
|---|---|
| `test_import_creates_cards_and_links` | `TestImport_ImportCreatesCardsAndLinks` |
| `test_ids_are_remapped` | `TestImport_IdsAreRemapped` |
| `test_edges_are_rewired_to_the_new_ids` | `TestImport_EdgesAreRewiredToTheNewIds` |
| `test_content_and_geometry_survive_the_round_trip` | `TestImport_ContentAndGeometrySurviveTheRoundTrip` |
| `test_import_reattributes_and_resets_rev` | `TestImport_ImportReattributesAndResetsRev` |
| `test_import_never_deletes` | `TestImport_ImportNeverDeletes` |
| `test_importing_twice_duplicates_rather_than_merging` | `TestImport_ImportingTwiceDuplicatesRatherThanMerging` |
| `test_import_emits_one_event_per_item` | `TestImport_ImportEmitsOneEventPerItem` |
| `test_an_edge_to_an_unknown_node_is_rejected_atomically` | `TestImport_AnEdgeToAnUnknownNodeIsRejectedAtomically` |
| `test_an_edge_may_reference_a_card_already_in_the_space` | `TestImport_AnEdgeMayReferenceACardAlreadyInTheSpace` |
| `test_empty_import_is_a_no_op` | `TestImport_EmptyImportIsANoOp` |
| `test_export_import_round_trips_through_a_second_space` | `TestImport_ExportImportRoundTripsThroughASecondSpace` |

## `test_links.py` → `links_test.go`

| python test | go test |
|---|---|
| `test_create_returns_json_canvas_edges` | `TestLinks_CreateReturnsJsonCanvasEdges` |
| `test_bulk_create` | `TestLinks_BulkCreate` |
| `test_links_appear_on_the_canvas` | `TestLinks_AppearOnTheCanvas` |
| `test_dangling_endpoints_are_404` | `TestLinks_DanglingEndpointsAre404` |
| `test_a_link_to_a_deleted_card_is_404` | `TestLinks_ALinkToADeletedCardIs404` |
| `test_a_rejected_batch_creates_nothing` | `TestLinks_ARejectedBatchCreatesNothing` |
| `test_delete_removes_the_link_from_the_canvas` | `TestLinks_DeleteRemovesTheLinkFromTheCanvas` |
| `test_deleting_twice_is_404` | `TestLinks_DeletingTwiceIs404` |
| `test_deleting_a_card_leaves_its_links_alone` | `TestLinks_DeletingACardLeavesItsLinksAlone` |
| `test_deleted_links_are_absent_even_with_include_deleted` | `TestLinks_DeletedLinksAreAbsentEvenWithIncludeDeleted` |

## `test_media.py` → `media_test.go`

| python test | go test |
|---|---|
| `test_upload_returns_a_url_and_metadata` | `TestMedia_UploadReturnsAUrlAndMetadata` |
| `test_the_returned_url_serves_the_bytes` | `TestMedia_TheReturnedUrlServesTheBytes` |
| `test_the_url_drops_into_a_file_node` | `TestMedia_TheUrlDropsIntoAFileNode` |
| `test_media_is_scoped_to_its_space` | `TestMedia_MediaIsScopedToItsSpace` |
| `test_unknown_media_is_404` | `TestMedia_UnknownMediaIs404` |
| `test_uploading_to_an_unknown_space_is_404` | `TestMedia_UploadingToAnUnknownSpaceIs404` |
| `test_upload_emits_no_event` | `TestMedia_UploadEmitsNoEvent` |
| `test_supported_types` | `TestMedia_SupportedTypes` |
| `test_an_unsupported_type_is_rejected` | `TestMedia_AnUnsupportedTypeIsRejected` |
| `test_a_traversal_filename_cannot_escape_the_media_directory` | `TestMedia_ATraversalFilenameCannotEscapeTheMediaDirectory` |

## `test_openapi.py` → `openapi_test.go`

| python test | go test |
|---|---|
| `test_spec_is_valid_openapi_31` | `TestOpenapi_SpecIsOpenapi31` ² |
| `test_every_spec_endpoint_is_documented` | `TestOpenapi_EverySpecEndpointIsDocumented` |
| `test_base_url_pins_the_port` | `TestOpenapi_BaseUrlPinsThePort` |
| `test_the_server_defaults_to_the_contracts_address` | `TestOpenapi_TheServerDefaultsToTheContractsAddress` |
| `test_mutating_operations_require_actor` | `TestOpenapi_MutatingOperationsRequireActor` |
| `test_feedback_requires_actor_but_not_actor_kind` | `TestOpenapi_FeedbackRequiresActorButNotActorKind` |
| `test_no_whole_canvas_replace` | `TestOpenapi_NoWholeCanvasReplace` |

## `test_revision_mode.py` → `revision_mode_test.go`

| python test | go test |
|---|---|
| `test_replace_mutates_in_place` | `TestRevision_ReplaceMutatesInPlace` |
| `test_replace_keeps_the_old_content_only_in_the_event_log` | `TestRevision_ReplaceKeepsTheOldContentOnlyInTheEventLog` |
| `test_branch_returns_the_new_card` | `TestRevision_BranchReturnsTheNewCard` |
| `test_branch_marks_the_old_card_and_freezes_it` | `TestRevision_BranchMarksTheOldCardAndFreezesIt` |
| `test_branch_keeps_both_cards_visible` | `TestRevision_BranchKeepsBothCardsVisible` |
| `test_branch_does_not_stack_the_new_card_on_the_old_one` | `TestRevision_BranchDoesNotStackTheNewCardOnTheOldOne` |
| `test_branch_auto_links_old_to_new_with_label_revised` | `TestRevision_BranchAutoLinksOldToNewWithLabelRevised` |
| `test_branch_emits_a_create_and_a_link_and_nothing_else` | `TestRevision_BranchEmitsACreateAndALinkAndNothingElse` |
| `test_branch_reports_as_a_new_card_not_an_edit` | `TestRevision_BranchReportsAsANewCardNotAnEdit` |
| `test_annotations_stay_on_the_card_they_were_made_on` | `TestRevision_AnnotationsStayOnTheCardTheyWereMadeOn` |
| `test_branch_mode_never_produces_a_stale_annotation` | `TestRevision_BranchModeNeverProducesAStaleAnnotation` |
| `test_feedback_still_delivers_annotations_on_superseded_cards` | `TestRevision_FeedbackStillDeliversAnnotationsOnSupersededCards` |
| `test_mode_query_overrides_a_replace_space` | `TestRevision_ModeQueryOverridesAReplaceSpace` |
| `test_mode_query_overrides_a_branch_space` | `TestRevision_ModeQueryOverridesABranchSpace` |
| `test_an_unknown_mode_is_rejected` | `TestRevision_AnUnknownModeIsRejected` |
| `test_a_move_never_branches` | `TestRevision_AMoveNeverBranches` |
| `test_if_match_still_applies_in_branch_mode` | `TestRevision_IfMatchStillAppliesInBranchMode` |
| `test_branching_a_superseded_card_is_rejected` | `TestRevision_BranchingASupersededCardIsRejected` |
| `test_annotations_on_a_superseded_card_name_the_replacement` | `TestRevision_AnnotationsOnASupersededCardNameTheReplacement` |

## `test_schema_sql.py` → `schema_sql_test.go`

| python test | go test |
|---|---|
| `test_schema_applies_cleanly` | `TestSchema_SchemaAppliesCleanly` |
| `test_table_columns` | `TestSchema_TableColumns` |
| `test_slug_is_unique` | `TestSchema_SlugIsUnique` |
| `test_revision_modes_accepted` | `TestSchema_RevisionModesAccepted` |
| `test_revision_mode_check_rejects_others` | `TestSchema_RevisionModeCheckRejectsOthers` |
| `test_event_type_check` | `TestSchema_EventTypeCheck` |
| `test_event_seq_is_unique_per_space` | `TestSchema_EventSeqIsUniquePerSpace` |
| `test_actor_kind_and_motivation_checks` | `TestSchema_ActorKindAndMotivationChecks` |
| `test_deleting_a_space_cascades` | `TestSchema_DeletingASpaceCascades` |
| `test_soft_delete_columns_default_null` | `TestSchema_SoftDeleteColumnsDefaultNull` |

## `test_spaces.py` → `spaces_test.go`

| python test | go test |
|---|---|
| `test_create_returns_201_and_a_valid_space` | `TestSpaces_CreateReturns201AndAValidSpace` |
| `test_create_accepts_branch_mode` | `TestSpaces_CreateAcceptsBranchMode` |
| `test_duplicate_slug_is_409` | `TestSpaces_DuplicateSlugIs409` |
| `test_invalid_slugs_are_rejected` | `TestSpaces_InvalidSlugsAreRejected` |
| `test_get_unknown_space_is_404` | `TestSpaces_GetUnknownSpaceIs404` |
| `test_counts_track_live_rows_only` | `TestSpaces_CountsTrackLiveRowsOnly` |
| `test_patch_updates_title_and_revision_mode` | `TestSpaces_PatchUpdatesTitleAndRevisionMode` |
| `test_space_created_names_the_space` | `TestSpaces_SpaceCreatedNamesTheSpace` |
| `test_delete_removes_the_space_and_its_contents` | `TestSpaces_DeleteRemovesTheSpaceAndItsContents` |
| `test_space_seq_is_the_event_counter` | `TestSpaces_SpaceSeqIsTheEventCounter` |
| `test_seq_is_per_space` | `TestSpaces_SeqIsPerSpace` |

## `conftest.py` → `harness_test.go` + `jsonschema_test.go`

Server lifecycle, the seed/token commands, actor params, `assert_valid` (a
validator for the schema-keyword subset openapi.json uses) and the shared
request shapes.

