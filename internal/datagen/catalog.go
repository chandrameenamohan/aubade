package datagen

// Catalog is the exam, in the order the scripts run.
//
// Order is not arbitrary and it is not the timeline: the plan re-sorts every
// artifact by time before anyone reads it, so this list is free to be the
// reading order a human wants — the positive tasks grouped by what they are
// about, then the negative half. A trap's position here never changes what it
// asserts.
//
// The bar the catalog is held to, asserted in datagen_test.go rather than
// hoped for:
//
//   - every extractor in model.KnownKinds is the expect.signal_kind of at least
//     one task, so no extractor can break unnoticed;
//   - at least twelve positive tasks and four negative ones (SPEC §1), because
//     an eval that only tests occurrence teaches an engine to say yes to
//     everything (EVAL-PRINCIPLES #8);
//   - every keyword a trap will be graded on is planted verbatim in that trap's
//     own artifacts, so every task is provably answerable from cited evidence
//     (EVAL-PRINCIPLES #7).
func Catalog() []Scenario {
	return []Scenario{
		// What Avery owes people.
		commitmentCapTableSlip,
		commitmentBoardUpdateOverdue,

		// What has gone quiet on her.
		quietInvestorAperture,
		cadenceSlowdownNorthstar,
		stalledDesignerLoop,

		// What her calendar is doing without her.
		conflictDoubleBooked,
		conflictDeepWorkBlock,
		conflictFamilyCollision,

		// Where two sources disagree, and where one cannot be aged.
		contradictionDiligenceCallDay,
		contradictionVeritasRenewal,
		stalenessUndatedModelNote,

		// What she can answer in under a minute, and the pattern hiding in P4.
		dispatchableHalberdYesNo,
		dispatchableExpenseApprovals,
		recruiterPatternKestrel,

		// The negative half: everything the digest must leave alone.
		negativeNewsletter,
		negativeVendorMarketing,
		negativeFYIOnly,
		negativeAveryLastWord,
		negativeAcceptedInvite,
		negativeQuietBelowThreshold,
	}
}
