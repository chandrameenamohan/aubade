package datagen

import (
	"fmt"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The mail filler. Two populations, generated separately because they behave
// nothing alike:
//
//   - **Noise** — newsletters, product marketing, machine notifications and
//     recruiter cold mail. One message, no thread, labelled the way real bulk
//     mail is labelled so the priority map floors it at P4 and the profile's own
//     suppression rules do the rest. This is ~30% of the inbox (SPEC §1), and it
//     is what makes "did the digest find the cap table" a question worth asking.
//   - **Conversations** — two to five messages between Avery and someone she
//     works with, always opened by the other side and always ending closed. See
//     the filler.go header for why closed is not laziness: an open filler thread
//     is an unscripted finding, and unscripted findings are the ones no
//     scorecard has an opinion about.

// mail tops the inbox up to TargetEmails, keeping the noise share at
// noisePercent of the *whole* corpus — the negative traps already planted some,
// so the filler adds the difference rather than its own 30%.
func (f *filler) mail() {
	want := TargetEmails - len(f.s.plan.Emails)
	if want <= 0 {
		return
	}
	wantNoise := TargetEmails*noisePercent/100 - noiseCount(f.s.plan.Emails)
	wantNoise = min(max(wantNoise, 0), want)
	if want-wantNoise == 1 {
		// One message is not a conversation, and an unanswered first contact is
		// an open thread. Give the odd one to the noise instead.
		wantNoise++
	}

	f.noiseMail(wantNoise)
	f.conversations(want - wantNoise)
}

// noiseMail emits n one-message bulk items across the corpus window.
//
// The first CorpusDays of them are dealt one per day. An inbox with a silent
// Tuesday is not an inbox, and a weighted draw leaves silent days often enough
// that "every day carries mail" would otherwise be a property of the seed
// rather than of the generator.
func (f *filler) noiseMail(n int) {
	for i := range n {
		day, covering := f.day(), i < len(f.days)
		if covering {
			day = f.days[i]
		}
		switch r := f.rng().IntN(100); {
		case r < 42:
			f.newsletter(day)
		case r < 68:
			f.marketing(day)
		// Recruiters write on working days, so a cold approach dealt to a
		// Saturday moves off it — which would leave the Saturday silent. The
		// covering pass therefore never draws one.
		case r < 88 || covering:
			f.automated(day)
		default:
			f.recruiter(day)
		}
	}
}

// bulkEmail plants one machine-sent message.
func (f *filler) bulkEmail(from model.Person, day time.Time, label, subject string, body func() string) {
	f.s.Email(model.Email{
		ID:       f.mailID(),
		ThreadID: f.threadID(),
		TS:       f.slotAfter(f.s.At(day, 4, 0), bulkWindows, false),
		From:     from,
		Subject:  subject,
		Body:     f.unique(subject, body),
		Labels:   []string{label},
	})
}

func (f *filler) newsletter(day time.Time) {
	p := pick(f, publications)
	subject := pick(f, p.subjects)
	f.bulkEmail(p.from, day, "newsletter", subject, func() string {
		also := pick(f, p.subjects)
		for also == subject && len(p.subjects) > 1 {
			also = pick(f, p.subjects)
		}
		return fmt.Sprintf("%s\n\nAlso in this issue: %s.\n\n%s",
			pick(f, p.leads), strings.ToLower(also), pick(f, newsletterFooters))
	})
}

func (f *filler) marketing(day time.Time) {
	v := pick(f, vendors)
	subject := pick(f, v.subjects)
	f.bulkEmail(v.from, day, "marketing", subject, func() string {
		return fmt.Sprintf("%s\n\n%s", pick(f, v.pitches), pick(f, marketingCTAs))
	})
}

func (f *filler) automated(day time.Time) {
	m := pick(f, machines)
	line := pick(f, m.events)
	// The reference number is what makes machine mail feel machine-sent, and it
	// is drawn rather than fixed so two build notifications are never the same
	// notification twice.
	ref := 1000 + f.rng().IntN(9000)
	subject := fmt.Sprintf(line.subject, ref)
	f.bulkEmail(m.from, day, "automated", subject, func() string {
		return fmt.Sprintf("%s\n\n%s", fmt.Sprintf(line.body, ref), pick(f, machineFooters))
	})
}

// recruiter plants one cold approach, keeping every firm but the scripted one
// below the profile's "three+ from the same firm in a week" threshold.
func (f *filler) recruiter(day time.Time) {
	firm := pick(f, recruitingFirms)
	who := pick(f, firm.people)
	day = f.businessDayOnOrBefore(day)

	if f.s.Today().Sub(day) <= patternWindow {
		if f.recentRecruiters[firm.name] >= patternHeadroom {
			// Move it out of the window rather than dropping it: the corpus
			// keeps its volume, and the only firm that reaches a pattern stays
			// the one a trap scripted.
			day = f.businessDayOnOrBefore(day.AddDate(0, 0, -8))
		} else {
			f.recentRecruiters[firm.name]++
		}
	}

	subject := pick(f, recruiterSubjects)
	f.bulkEmail(who, day, "recruiter", subject, func() string {
		return fmt.Sprintf("%s %s\n\n%s\n%s, %s",
			pick(f, recruiterOpeners), pick(f, recruiterPitches), who.Name, firm.role, firm.name)
	})
}

// patternWindow and patternHeadroom mirror the profile rule the suppression
// extractor implements: three from one firm inside a week is a pattern, so two
// is the most an unscripted firm may reach.
const (
	patternWindow   = 7 * 24 * time.Hour
	patternHeadroom = 2
)

// businessDayOnOrBefore walks back to a working day. Cold outreach and
// colleagues' mail arrive on working days; walking *back* rather than forward
// keeps everything inside the corpus window without special-casing the anchor.
func (f *filler) businessDayOnOrBefore(day time.Time) time.Time {
	for !isBusinessDay(day) {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

// conversations emits threaded mail until n messages have been planted.
//
// Topics are used in rotation rather than sampled, so every one of them appears
// and the corpus covers the whole surface of Avery's week; a repeat carries the
// week it belongs to in the subject, which is how recurring business threads
// actually read.
func (f *filler) conversations(n int) {
	for emitted := 0; emitted < n; {
		remaining := n - emitted
		length := min(pick(f, threadLengths), remaining)
		if remaining-length == 1 {
			// Never leave one message over: the thread that inherited it would
			// be an unanswered first contact, which is an open thread.
			length = remaining
		}
		idx := f.threadSeq
		t := conversationTopics[idx%len(conversationTopics)]
		emitted += f.conversation(t, idx/len(conversationTopics), length)
	}
}

// threadLengths is the shape of a working inbox: mostly a question and an
// answer, sometimes an exchange.
var threadLengths = []int{2, 2, 2, 3, 3, 3, 4, 4, 5}

// conversation plants one closed thread and returns how many messages it wrote.
func (f *filler) conversation(t topic, occurrence, want int) int {
	with := pick(f, t.cast)
	subject := t.subject
	// A conversation opens before the anchor day so that its first reply always
	// has somewhere to land: a thread truncated to its opening message is an
	// unanswered first contact, which is the one shape this layer must not
	// plant. Nothing is lost — the digest anchors at 06:00, so mail that
	// arrives later today is already outside what it reasons about.
	day := f.businessDayOnOrBefore(f.day())
	if !day.Before(f.s.Today()) {
		day = f.businessDayOnOrBefore(f.s.Days(-1))
	}
	if occurrence > 0 {
		subject = fmt.Sprintf("%s — week of %s", t.subject, weekOf(day).Format("Jan 2"))
	}

	// The timeline is laid out before a single message is written, so the
	// message that *closes* the thread is known in advance. Deciding it as we
	// go would mean a thread that runs out of days ends on whatever it happened
	// to be saying — a mid-thread follow-up that leaves something open, which is
	// exactly the finding this layer must not plant.
	times := f.threadTimes(day, want)

	thread := f.threadID()
	last := f.s.Email(model.Email{
		ID:       f.mailID(),
		ThreadID: thread,
		TS:       times[0],
		From:     with,
		Subject:  subject,
		Body:     f.unique(subject, func() string { return sign(pick(f, t.openers), with) }),
		Labels:   t.labels,
	})

	replySubject := "Re: " + subject
	for i := 1; i < len(times); i++ {
		fromOwner := i%2 == 1
		closing := i == len(times)-1

		from, recipients := with, to(Avery)
		body := func() string { return f.counterpartLine(closing, with) }
		if fromOwner {
			from, recipients, body = Avery, to(with), f.ownerLine
		}

		last = f.s.Email(model.Email{
			ID:        f.mailID(),
			ThreadID:  thread,
			TS:        times[i],
			From:      from,
			To:        recipients,
			Subject:   replySubject,
			Body:      f.unique(replySubject, body),
			InReplyTo: last.Ref,
			Labels:    t.labels,
		})
	}
	return len(times)
}

// threadTimes lays out when each message in a thread arrives: the opener in the
// other side's working day, then alternating replies, each in its sender's own
// windows. The run stops at the corpus edge, so a thread started late is short
// rather than dated into the future.
func (f *filler) threadTimes(day time.Time, want int) []time.Time {
	times := []time.Time{f.slotAfter(f.s.At(day, 7, 0), workWindows, true)}
	for i := 1; i < want; i++ {
		windows, businessOnly, gap := workWindows, true, time.Duration(2+f.rng().IntN(28))*time.Hour
		if i%2 == 1 {
			windows, businessOnly, gap = ownerWindows, false, time.Duration(20+f.rng().IntN(600))*time.Minute
		}
		next := f.slotAfter(times[len(times)-1].Add(gap), windows, businessOnly)
		if next.After(f.lastInstant()) {
			break
		}
		times = append(times, next)
	}
	return times
}

// ownerLine is Avery answering: short, lowercase, no question, nothing promised.
// The tone is profile.md's ("Three sentences is normal. Two is fine. One is
// great."), and the constraint is the extractors' — a promise with a date in it
// is a commitment, and a question is an open thread.
func (f *filler) ownerLine() string {
	return pick(f, ownerReplies) + pick(f, ownerSignoffs)
}

// counterpartLine is the other side speaking. Mid-thread it may ask for
// something; the closing one never does.
func (f *filler) counterpartLine(closing bool, who model.Person) string {
	if closing {
		return sign(pick(f, counterpartCloses), who)
	}
	return sign(pick(f, counterpartFollowups), who)
}

// sign adds the sender's first name under the message, the way half of real
// mail is signed and half is not.
func sign(body string, who model.Person) string {
	if strings.Contains(body, "\n") || len(who.Name) == 0 {
		return body
	}
	first, _, _ := strings.Cut(who.Name, " ")
	return body + "\n\n" + first
}

// weekOf is the Monday of the week a day falls in — the label a recurring
// thread carries in its subject.
func weekOf(day time.Time) time.Time {
	for day.Weekday() != time.Monday {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

// --- Noise: the senders and the phrasing ------------------------------------

// publication is one mailing list Avery subscribed to and does not want in her
// digest ("Newsletters. Even the good ones.").
type publication struct {
	from     model.Person
	subjects []string
	leads    []string
}

var publications = []publication{
	{
		from: Stratechery,
		subjects: []string{
			"The Weekly Update: aggregation in industrials",
			"Vertical software and the cost of switching",
			"Why the ERP incumbents keep winning procurement",
			"Interconnects: manufacturing data, three ways",
			"The margin question nobody asks about supply chain",
			"An interview about industrial pricing power",
		},
		leads: []string{
			"The interesting thing about vertical software is not the vertical. It is that the buyer has nowhere else to go once the data lives with you, and that fact shows up in the renewal line long before it shows up in the pitch.",
			"Three quarters of manufacturing software M&A this year went to buyers who already owned the system of record. That is not consolidation. That is the record holder buying its own optionality back.",
			"Procurement is where software companies find out whether they sold a product or a project. The distinction is invisible on a demo and obvious on an invoice.",
			"There is a version of the supply-chain visibility story where the data is the moat, and a version where the workflow is. This week: why the second one keeps outlasting the first.",
		},
	},
	{
		from: model.Person{Name: "Supply Chain Dive", Email: "newsletter@supplychaindive.example"},
		subjects: []string{
			"Mid-market manufacturers slow capex again",
			"Port throughput steadies after a rough quarter",
			"Tariff guidance lands for tier-2 suppliers",
			"Warehouse automation orders soften",
			"Freight rates hold, contracts get shorter",
			"Five charts on inventory turns",
		},
		leads: []string{
			"Capital spending intentions among mid-market manufacturers fell for a third consecutive quarter, with software the one line most respondents said they would protect.",
			"Contract lengths are shortening across freight and warehousing. Buyers say they want the option to move; sellers say they want the volume. Both get less of what they wanted.",
			"Inventory turns improved for the first time since spring, though the improvement is concentrated in firms that spent last year writing down what they could not sell.",
			"The guidance published this week clarifies reporting for tier-2 suppliers and leaves the tier-3 question open for another year.",
		},
	},
	{
		from: model.Person{Name: "The Pragmatic Engineer", Email: "newsletter@pragmaticeng.example"},
		subjects: []string{
			"What twelve-person engineering teams get right",
			"On-call without a rotation is not on-call",
			"The hiring bar nobody writes down",
			"Postmortems that people actually read",
			"Infra costs at Series A",
			"Deploy on Friday, or don't",
		},
		leads: []string{
			"Small teams beat large ones on latency of decision, and lose to them on depth of coverage. The interesting part is that most companies pick the wrong one of those to optimise as they grow.",
			"An on-call rotation with two names on it is a rota in the same way that a coin flip is a decision procedure. It works right up until one of the names is on a plane.",
			"Every engineering organisation has a hiring bar it can describe and a hiring bar it actually uses. The gap between them is where the offer letters get argued about.",
			"The best postmortems this year had one thing in common: they named the decision, not the person, and they were shorter than a page.",
		},
	},
	{
		from: model.Person{Name: "SaaS Metrics Weekly", Email: "newsletter@saasmetrics.example"},
		subjects: []string{
			"Net revenue retention is still the whole game",
			"Benchmarks: $1M to $5M ARR",
			"Payback periods lengthen across the cohort",
			"Why logo churn misleads at this stage",
			"Seat expansion versus usage expansion",
			"The board metric everyone reports differently",
		},
		leads: []string{
			"Across the cohort between one and five million in ARR, median net revenue retention held at 108% while gross retention slipped two points. The spread between those two numbers is where the story is.",
			"Payback lengthened again this quarter. Nothing about the sales motion changed; the deals just got smaller and the discounting got quieter.",
			"Logo churn is the metric that flatters small companies and indicts large ones. At this stage, revenue churn tells you far more and is far less fun to put on a slide.",
			"Seat expansion is easier to model and harder to sustain than usage expansion. Most of the companies that grew well this year did the harder one.",
		},
	},
	{
		from: model.Person{Name: "Term Sheet Daily", Email: "newsletter@termsheetdaily.example"},
		subjects: []string{
			"Series A pace steadies, valuations do not",
			"Two funds close, one quietly does not",
			"Bridge rounds are back and nobody says so",
			"What LPs are asking about this quarter",
			"Structure creeps into growth rounds",
			"The diligence question of the season",
		},
		leads: []string{
			"Series A deal count was flat month over month, but the median pre-money moved almost a full turn. Averages are doing a lot of work in that sentence.",
			"Two funds announced closes this week. A third has been raising for fourteen months and has stopped putting out a target.",
			"Structure is showing up in growth rounds again — liquidation preferences that would have been embarrassing eighteen months ago, now merely unremarkable.",
			"The diligence question of the season is gross margin composition. Founders who can decompose it in one slide are getting through in half the meetings.",
		},
	},
	{
		from: model.Person{Name: "Manufacturing Monitor", Email: "newsletter@mfgmonitor.example"},
		subjects: []string{
			"Plant-floor connectivity, one year on",
			"MES replacement cycles are getting longer",
			"Quality data leaves the spreadsheet",
			"Labour shortages reshape shift planning",
			"Predictive maintenance, honestly assessed",
			"Regional reshoring by the numbers",
		},
		leads: []string{
			"A year after the connectivity push, most plants report the sensors are installed and the data is unused. The gap is not technical and never was.",
			"MES replacement cycles have stretched past nine years in the mid-market. Anyone selling into that timeline is selling into a window that opens once a decade.",
			"Quality data is finally leaving the spreadsheet, mostly because the person who maintained the spreadsheet retired.",
			"Predictive maintenance results this year were real but modest: fewer unplanned stoppages, roughly unchanged total downtime, and a lot of dashboards.",
		},
	},
}

var newsletterFooters = []string{
	"You are receiving this because you subscribed. Unsubscribe at any time.",
	"Forwarded to you — subscribe here. Manage your preferences at the link below.",
	"Reply to this email to reach the editor. Unsubscribe with one click.",
	"Sent to avery@tessera.io. Update your delivery preferences or unsubscribe.",
}

// vendor is a SaaS tool sending product marketing — including the ones Tessera
// pays for, which is the clause that makes the suppression rule interesting.
type vendor struct {
	from     model.Person
	subjects []string
	pitches  []string
}

var vendors = []vendor{
	{
		from: Pagerail,
		subjects: []string{
			"Pagerail changelog: September",
			"Your incident response, reviewed",
			"New: alert grouping that actually groups",
			"Pagerail + your on-call rotation",
			"Three ways teams cut alert noise this year",
		},
		pitches: []string{
			"Your team is on the Pagerail Team plan. This month we shipped alert grouping, a rewritten mobile view, and incident timelines you can export.",
			"Teams like yours cut alert volume by a third after turning on grouping. It takes about ten minutes to configure and nothing to maintain.",
			"We rebuilt the incident timeline from scratch. Every page, ack and resolve now lands on one scrollable view you can hand to a customer.",
			"Your on-call rotation has two people in it. Here is what teams your size usually do about that before it becomes a retention problem.",
		},
	},
	{
		from: model.Person{Name: "Ledgerline", Email: "marketing@ledgerline.example"},
		subjects: []string{
			"Close the books four days faster",
			"Ledgerline for Series A finance teams",
			"Your revenue recognition, automated",
			"A shorter month-end close",
			"Webinar: SaaS metrics your board will ask about",
		},
		pitches: []string{
			"Finance teams at Series A companies close in nine days on average. Our customers close in five, and none of them added headcount to do it.",
			"Revenue recognition is where a clean month-end goes to die. Ledgerline reads your contracts and does the schedule for you.",
			"We put together a walkthrough of the metrics investors ask for at Series A, with the definitions written down so nobody argues about them twice.",
			"Your accountant is exporting CSVs. There is a version of this where they do not.",
		},
	},
	{
		from: model.Person{Name: "Northwind CRM", Email: "hello@northwindcrm.example"},
		subjects: []string{
			"Pipeline hygiene for small GTM teams",
			"Northwind: what shipped this quarter",
			"Your CRM is lying to you about forecast",
			"Two-person sales teams, ten-person output",
			"Book a walkthrough with your account team",
		},
		pitches: []string{
			"Forecast accuracy at companies your size averages 41%. The problem is almost never the model; it is that nobody updates the stage.",
			"This quarter we shipped territory rules, a pipeline hygiene score, and a forecast view your head of GTM can actually defend.",
			"Small GTM teams do not need more fields. They need fewer, and a reason to fill them in.",
			"You have been on the free tier for four months. Here is what the paid tier changes, in one paragraph.",
		},
	},
	{
		from: model.Person{Name: "Cloudmere", Email: "updates@cloudmere.example"},
		subjects: []string{
			"Cloudmere pricing update, effective next quarter",
			"Committed-use discounts for growing teams",
			"New region: us-west-2",
			"Your infrastructure spend, benchmarked",
			"Cloudmere for data-heavy workloads",
		},
		pitches: []string{
			"We are simplifying pricing next quarter: fewer line items, one egress rate, and committed-use discounts that start lower than they used to.",
			"Your workload profile looks like it would benefit from committed use. Teams with your shape typically save around 18%.",
			"A new region is live. Latency to the Bay Area drops by roughly a third for workloads that move today.",
			"We benchmarked infrastructure spend across two thousand teams. The median Series A company spends more on storage than on compute, and is surprised by it.",
		},
	},
	{
		from: model.Person{Name: "Deskly", Email: "no-reply@deskly.example"},
		subjects: []string{
			"Deskly: support that scales past the founder",
			"Your first support playbook",
			"Macros, snippets and the end of copy-paste",
			"Deskly is now part of your inbox",
			"Support metrics that mean something",
		},
		pitches: []string{
			"At twelve people, support is still somebody's second job. Deskly makes it a repeatable one before it becomes a full-time one.",
			"We wrote the support playbook we wish we had at your stage. It is eleven pages and free.",
			"Macros sound trivial until you count how many times your team typed the same four sentences last month.",
			"First response time is the metric everyone tracks and the one customers notice least. Here is what they actually notice.",
		},
	},
	{
		from: model.Person{Name: "Kanto Analytics", Email: "marketing@kanto.example"},
		subjects: []string{
			"Product analytics without the instrumentation project",
			"Kanto: funnels in an afternoon",
			"What your onboarding funnel is hiding",
			"Session replay, now with less noise",
			"A pricing change you will like",
		},
		pitches: []string{
			"Instrumentation projects fail because they are projects. Kanto reads what your app already emits and builds the funnel from that.",
			"Most onboarding funnels lose people at a step nobody in the company has looked at in six months. Ours tells you which one.",
			"Session replay generated more noise than insight for most teams. We rebuilt it to surface twelve sessions instead of twelve hundred.",
			"We lowered the entry tier and removed the event cap. Nothing else changed.",
		},
	},
}

var marketingCTAs = []string{
	"Book a walkthrough with your account team.",
	"Start a trial — no card, no call.",
	"Read the full post on our blog.",
	"Reply to this email and we will set something up.",
	"See what changed in the changelog.",
}

// machine is an automated sender: build systems, billing, alerting.
type machine struct {
	from   model.Person
	events []machineEvent
}

// machineEvent is one notification template. Both halves take the same
// reference number, which is what keeps two build notifications from being the
// same notification twice.
type machineEvent struct{ subject, body string }

var machines = []machine{
	{
		from: model.Person{Name: "Tessera CI", Email: "noreply@ci.tessera.io"},
		events: []machineEvent{
			{"Build #%d passed on main", "Pipeline #%d completed successfully in 6m41s. 412 tests, 0 failures."},
			{"Build #%d failed on main", "Pipeline #%d failed at the integration stage. One test timed out; a retry has been queued automatically."},
			{"Nightly job #%d completed", "The nightly export job #%d finished in 22m. No records were rejected."},
			{"Deploy #%d promoted to production", "Release #%d is live in production. Rollback remains available for 24 hours."},
		},
	},
	{
		from: model.Person{Name: "Cloudmere Billing", Email: "no-reply@billing.cloudmere.example"},
		events: []machineEvent{
			{"Invoice CM-%d is available", "Invoice CM-%d for your Cloudmere account is ready. Payment will be collected from the card on file in seven days."},
			{"Usage summary for account %d", "Your account %d used 68%% of its committed compute this period. Storage grew 4%% month over month."},
			{"Payment received — receipt %d", "We received your payment. Receipt %d is attached to your billing portal."},
		},
	},
	{
		from: model.Person{Name: "Pagerail Alerts", Email: "notifications@pagerail.example"},
		events: []machineEvent{
			{"Incident #%d resolved", "Incident #%d was acknowledged in 4m and resolved in 19m. Ingest latency returned to baseline."},
			{"Weekly on-call summary (#%d)", "Report #%d: 3 pages this week, all acknowledged inside the target window. No escalations."},
			{"Monitor #%d recovered", "Monitor #%d recovered after 6 minutes. No customer-facing errors were recorded."},
		},
	},
	{
		from: model.Person{Name: "Tessera Identity", Email: "noreply@id.tessera.io"},
		events: []machineEvent{
			{"New sign-in from Oakland, CA (#%d)", "Session %d was started from a recognised device in Oakland, California. No action is needed if this was you."},
			{"Recovery codes regenerated (#%d)", "Request %d regenerated your recovery codes. The previous set is no longer valid."},
			{"Access review #%d is open", "Quarterly access review %d is open. Nine accounts are in scope and no removals are pending."},
		},
	},
	{
		from: model.Person{Name: "Statuspage", Email: "updates@status.cloudmere.example"},
		events: []machineEvent{
			{"Scheduled maintenance window #%d", "Maintenance %d is scheduled for the us-west region. Expected impact: none for multi-zone workloads."},
			{"Degraded performance resolved (#%d)", "Event %d is closed. Elevated API latency lasted 12 minutes and has returned to normal."},
		},
	},
}

var machineFooters = []string{
	"This is an automated message. Do not reply.",
	"You are receiving this because you own this account.",
	"Manage notification settings in your account preferences.",
	"Sent automatically by the platform. Replies are not monitored.",
}

// recruitingFirm is a search firm cold-emailing the CEO. Every firm here stays
// below the profile's pattern threshold inside any one week — the only firm
// that reaches three is the one a scenario scripted.
type recruitingFirm struct {
	name   string
	role   string
	people []model.Person
}

var recruitingFirms = []recruitingFirm{
	{
		name: "Ridgeline Talent", role: "Partner",
		people: []model.Person{
			{Name: "Marta Ruiz", Email: "marta@ridgelinetalent.example"},
			{Name: "Devon Hart", Email: "devon@ridgelinetalent.example"},
		},
	},
	{
		name: "Beacon Executive Search", role: "Principal",
		people: []model.Person{
			{Name: "Nils Ostergaard", Email: "nils@beaconexec.example"},
			{Name: "Grace Amadi", Email: "grace@beaconexec.example"},
		},
	},
	{
		name: "Northgate Partners", role: "Managing Director",
		people: []model.Person{
			{Name: "Hollis Byrne", Email: "hollis@northgatepartners.example"},
		},
	},
	{
		name: "Vantage Talent Group", role: "Recruiter",
		people: []model.Person{
			{Name: "Sofia Klein", Email: "sofia@vantagetalent.example"},
			{Name: "Ray Okonkwo", Email: "ray@vantagetalent.example"},
		},
	},
}

var recruiterSubjects = []string{
	"senior backend engineers, Bay Area",
	"quick note about your engineering hiring",
	"design leadership candidates for Tessera",
	"platform engineers who know supply chain",
	"introductions for your Series A team build",
	"a candidate I think you should meet",
	"following up on my note last month",
	"staff engineers open to a 12-person team",
}

var recruiterOpeners = []string{
	"Hope you don't mind the cold note.",
	"Saw the Tessera hiring page and thought I would reach out.",
	"We have not met — apologies for the interruption.",
	"Congratulations on the momentum this year.",
	"Happy to be told this is not a fit.",
}

var recruiterPitches = []string{
	"We place engineering and design talent at Series A companies and have three people who would fit a team of your size.",
	"I run searches for infrastructure and platform roles in the Bay Area and keep a bench of people who have shipped at this stage before.",
	"Our practice focuses on first design hires. Two of the people we placed last quarter came out of supply-chain software.",
	"We work on retained searches for founding-team roles and are happy to share a shortlist with no commitment.",
	"I have two candidates finishing notice periods who asked specifically about companies doing industrial data.",
}

// --- Conversations: the topics Avery's week is actually made of --------------

// topic is one recurring strand of Avery's working life. cast is who might
// raise it; openers are the ways it gets raised, one per occurrence, so a
// recurring thread does not repeat itself.
type topic struct {
	subject string
	cast    []model.Person
	labels  []string
	openers []string
}

var (
	internalCast = []model.Person{Priya, Jordan, Tomas, Nadia}
	engCast      = []model.Person{Priya, Jordan}
	gtmCast      = []model.Person{Tomas, Nadia}
	customerCast = []model.Person{Renee, Dana, Luis}
	outsideCast  = []model.Person{Ines, Lumen}
	raiseCast    = []model.Person{Marcus, Diane, Ben, David}
)

var conversationTopics = []topic{
	{"staging environment is out of date", engCast, []string{"internal"}, []string{
		"Staging has drifted about three weeks behind production. Rebuilding it costs half a day and I would rather spend that day now than during a customer trial.",
		"Staging drifted again. I would like to make the rebuild part of the release checklist so it stops being a decision.",
	}},
	{"SOC 2 evidence collection", internalCast, []string{"internal"}, []string{
		"The auditor came back with a list of nine evidence items. Seven are screenshots I can pull myself; two need someone with admin on billing.",
		"Evidence collection is done except for the access review export. Nadia has the admin rights for that one.",
	}},
	{"support queue — weekly numbers", gtmCast, []string{"internal"}, []string{
		"Support queue closed the week at 14 open tickets, down from 22. Two of them are the same Halberd export question asked twice.",
		"Queue is at 11 open, median first response under three hours. Nothing is aging past a week now.",
	}},
	{"onboarding funnel drop-off", internalCast, []string{"internal", "product"}, []string{
		"Half the accounts that start onboarding stall at the connector step. The instructions assume a permission most ops leads do not have.",
		"Drop-off at the connector step improved four points after the copy change. Still the worst step in the funnel by some distance.",
	}},
	{"postmortem: ingest latency spike", engCast, []string{"internal"}, []string{
		"Wrote up the ingest latency spike. Root cause was a retry storm from one tenant, and the fix is a per-tenant cap we should have had already.",
		"Postmortem is written and short. One action came out of it and it is already merged.",
	}},
	{"contractor invoice for the design sprint", gtmCast, []string{"internal", "finance"}, []string{
		"The design contractor's invoice came in at the number we agreed. It sits in the queue until someone with the card signs it off.",
		"Second invoice from the design sprint arrived. Same rate, fewer days, nothing unexpected in it.",
	}},
	{"laptop refresh budget", internalCast, []string{"internal", "finance"}, []string{
		"Four laptops are out of warranty this quarter. Replacing them costs about nine thousand and can wait a month without anyone noticing.",
		"Two of the four laptops are now failing to hold charge through a day. I would move the refresh forward.",
	}},
	{"pricing page copy", gtmCast, []string{"internal", "product"}, []string{
		"New pricing page copy is drafted. It says the same thing in a third of the words and stops apologising for the enterprise tier.",
		"Pricing copy is live. Bounce rate on that page dropped, though a week is not a result.",
	}},
	{"backend candidate debrief", engCast, []string{"hiring"}, []string{
		"Debrief is in for the backend candidate. Two strong yeses, one weak no, and the weak no is about system design depth rather than anything cultural.",
		"Second backend candidate debriefed. Cleaner interview, less relevant experience, and the panel split down the middle.",
	}},
	{"data retention policy draft", internalCast, []string{"internal"}, []string{
		"Drafted the retention policy. Ninety days for raw events, eighteen months for aggregates, and a documented deletion path for anything a customer asks about.",
		"Retention policy is in review. Legal had one comment about the deletion window and nothing else.",
	}},
	{"office lease — landlord response", gtmCast, []string{"internal"}, []string{
		"The landlord came back on the lease. They will do two years at the current rate or five at a discount, and they want an answer before the quarter ends.",
		"Landlord moved on the fit-out contribution. Everything else in the draft is unchanged from last time.",
	}},
	{"warehouse costs are creeping", engCast, []string{"internal"}, []string{
		"Analytics warehouse spend is up 40% over two months and almost all of it is one dashboard refreshing every fifteen minutes.",
		"Warehouse costs are back to flat after the refresh schedule change. No queries got slower.",
	}},
	{"release notes for 4.2", engCast, []string{"internal", "product"}, []string{
		"Release notes for 4.2 are drafted. Three customer-visible changes and one deprecation that needs a sentence somebody outside engineering can read.",
		"4.2 notes are published. The deprecation got its own paragraph and a migration link.",
	}},
	{"customer webinar — logistics track", gtmCast, []string{"internal", "marketing"}, []string{
		"Sixty-one people registered for the logistics webinar, which is double the last one. Two of them are from accounts in the pipeline.",
		"Webinar recording is edited. Attendance held at 38% which is about par for the format.",
	}},
	{"on-call rotation for next month", engCast, []string{"internal"}, []string{
		"Next month's on-call rotation is drafted. It is still only two names deep, which works until one of them takes a holiday.",
		"Rotation is published. Adding a third name would mean pulling someone off the ingest work for a fortnight.",
	}},
	{"vendor security review: Cloudmere", internalCast, []string{"internal"}, []string{
		"Cloudmere's security package came back clean apart from a subprocessor we have not reviewed. Their answers were fast and specific.",
		"Security review on Cloudmere is closed. The subprocessor question resolved itself when they dropped the subprocessor.",
	}},
	{"demo environment keeps breaking", gtmCast, []string{"internal"}, []string{
		"The demo environment broke twice this week, both times mid-call. It shares a database with staging and that is the whole story.",
		"Demo environment has its own data now. It has survived four calls without an incident.",
	}},
	{"API reference rewrite", engCast, []string{"internal", "product"}, []string{
		"The API reference rewrite is two thirds done. Every endpoint now has a working example, which is more than the old one managed for half of them.",
		"Docs rewrite is finished and deployed. Search inside the docs is the next obvious gap.",
	}},
	{"payroll cutoff this month", internalCast, []string{"internal", "finance"}, []string{
		"Payroll cutoff moves forward two days this month because of the holiday. Nothing else about the run changes.",
		"Payroll is submitted. The two contractor payments went on the same run rather than a separate one.",
	}},
	{"quarterly OKR draft", internalCast, []string{"internal"}, []string{
		"First draft of the quarterly OKRs. Three objectives, nine results, and one of the objectives is really a project wearing a costume.",
		"OKR draft is down to two objectives after the trim. It reads better and commits us to less.",
	}},
	{"churn analysis — SMB tier", gtmCast, []string{"internal"}, []string{
		"Pulled the SMB churn analysis. Almost every cancellation came from an account that never connected a second data source.",
		"Churn analysis updated with last month's cancellations. The single-source pattern holds and is now hard to argue with.",
	}},
	{"new starter equipment", internalCast, []string{"internal"}, []string{
		"Equipment for the new backend hire is ordered and arrives the week before the start date. Everything else in the onboarding checklist is people, not hardware.",
		"Onboarding checklist is updated after the last two starts. It lost four steps and gained one that actually mattered.",
	}},
	{"bug triage backlog", engCast, []string{"internal"}, []string{
		"Triage backlog is at 40 open bugs. Six are customer-reported, the rest are ours, and about half are older than the code they describe.",
		"Backlog is down to 24 after the sweep. We closed anything untouched for six months and nobody has complained yet.",
	}},
	{"marketing site redesign quote", gtmCast, []string{"internal", "marketing"}, []string{
		"The agency quoted twenty-eight thousand for the site redesign, which is roughly twice what I expected and about half what the last agency wanted.",
		"Second quote for the site work came in materially lower with a longer timeline. Same scope, different bet.",
	}},
	{"API rate limits on the bulk endpoint", customerCast, []string{"customer"}, []string{
		"Our integration team is hitting the rate limit on the bulk endpoint during the nightly sync. They can batch differently if that is the better answer.",
		"The rate limit change went in and the nightly sync now finishes inside the window. No further issues from our side.",
	}},
	{"additional seats for the ops team", customerCast, []string{"customer"}, []string{
		"We would like eight more seats for the ops team, effective at the start of next month. Same tier, no other changes.",
		"The extra seats are all in use already. Adoption is better than we expected on the ops side.",
	}},
	{"SSO rollout timing", customerCast, []string{"customer"}, []string{
		"Our IT group wants everything behind SSO by the end of the quarter. Your documentation covers the setup; I want to make sure nothing else is needed from you.",
		"SSO is enabled for everyone here. The rollout was uneventful, which is the best outcome available.",
	}},
	{"invoice PO number correction", customerCast, []string{"customer", "finance"}, []string{
		"The last invoice carried the old PO number, so our accounts team bounced it. A reissue with the correct number clears it.",
		"The corrected invoice went through our system without a problem. Nothing further needed from you.",
	}},
	{"quarterly business review agenda", customerCast, []string{"customer"}, []string{
		"For the next business review I would like to spend most of the time on adoption in the plants rather than on the roadmap.",
		"The review agenda works for us. Our operations director will join for the first half.",
	}},
	{"sandbox credentials for our integrator", customerCast, []string{"customer"}, []string{
		"Our systems integrator needs sandbox credentials for two of their engineers. They are under our NDA already.",
		"The integrator has what they need and has finished their first pass. No changes requested to the schema.",
	}},
	{"export format for the audit team", customerCast, []string{"customer"}, []string{
		"Our internal audit team wants the export as CSV with a header row rather than the JSON bundle. Everything else about it is fine.",
		"The CSV export is exactly what audit wanted. They have signed off on the evidence for this cycle.",
	}},
	{"training session for new users", customerCast, []string{"customer"}, []string{
		"We have eleven new users starting on the platform next month and would like one training session rather than eleven onboarding calls.",
		"The training session went well. The recording is doing more work than the session did.",
	}},
	{"pilot scope for the logistics team", outsideCast, []string{"prospect"}, []string{
		"We would like to scope a pilot for the logistics team — two sites, one data source, six weeks. Nothing bigger until that works.",
		"The pilot scope looks right to us. Our logistics lead has approved the two sites internally.",
	}},
	{"reference call request", outsideCast, []string{"prospect"}, []string{
		"Before we go further, our team would like a reference call with a manufacturer of a similar size. Happy to work around whoever is willing.",
		"The reference call was useful and candid. It answered the questions our operations side had been circling.",
	}},
	{"integration questions from our platform team", outsideCast, []string{"prospect"}, []string{
		"Our platform team has read the API docs and has two structural questions about how you model sites versus facilities.",
		"Your answers on the site model resolved it. Our team is comfortable with the shape of the integration.",
	}},
	{"renewal quote for the analytics add-on", outsideCast, []string{"vendor"}, []string{
		"Here is the renewal quote for the analytics add-on. The rate is unchanged and the term matches your main agreement.",
		"The revised quote reflects the seat count we discussed. Nothing else about the agreement has moved.",
	}},
	{"data room folder structure", raiseCast, []string{"raise"}, []string{
		"The data room structure looks reasonable. One suggestion: split the customer contracts by segment so the diligence team is not scrolling.",
		"Folder structure is settled. Everything from the last raise has been carried over and re-labelled.",
	}},
	{"option pool mechanics", raiseCast, []string{"raise", "legal"}, []string{
		"On the option pool: the mechanics matter more than the headline number, and the pre-money treatment is where founders usually lose ground without noticing.",
		"The pool question is now just arithmetic. Both sides are working from the same model, which is most of the battle.",
	}},
	{"board deck template", raiseCast, []string{"board"}, []string{
		"Here is the board deck template we use with other portfolio companies. Twelve slides, and the last three are the only ones anyone argues about.",
		"The template worked well last cycle. The metrics page is the one I would keep exactly as it is.",
	}},
	{"intro to a logistics operator", raiseCast, []string{"raise"}, []string{
		"I know an operator who ran supply chain at a mid-market manufacturer for a decade and now advises. Worth an introduction if that profile is useful.",
		"The introduction has been made and they were enthusiastic about the space. Nothing owed on our side.",
	}},
	{"409A refresh timing", raiseCast, []string{"legal", "raise"}, []string{
		"On the 409A: refreshing it before the round closes is cleaner than refreshing it after, and the cost difference is negligible.",
		"The 409A refresh is complete and filed. The valuation landed roughly where we expected it to.",
	}},
	{"investor update format", raiseCast, []string{"board"}, []string{
		"The monthly update format you use is good. The only thing I would add is a line on cash and a line on hiring, every time, even when neither moved.",
		"The updated format reads well. Metrics first, narrative second, asks at the end is the right order.",
	}},
}

// ownerReplies are Avery answering, in the voice profile.md describes. None of
// them asks anything and none of them promises anything with a date attached:
// the closing message of a filler thread must not become a finding.
var ownerReplies = []string{
	"makes sense.",
	"agreed.",
	"yes, do that.",
	"fine by me.",
	"good — that helps.",
	"no objection.",
	"ship it.",
	"that's the right read.",
	"keep going.",
	"sounds right.",
	"noted.",
	"go with the second option.",
	"leave it as it is for now.",
	"reasonable.",
	"good outcome.",
	"understood.",
	"that works.",
	"clean. thank you.",
	"right call.",
	"do the cheaper one.",
	"not this quarter.",
	"take it.",
}

var ownerSignoffs = []string{
	"",
	"\n\nAvery",
	"\n\n— A",
	"\n\nthanks.\n\nAvery",
}

// counterpartFollowups are mid-thread messages. They are allowed to ask for
// things; only the last message in a thread is constrained.
var counterpartFollowups = []string{
	"One caveat: the numbers above are through last week, not month-end.",
	"Adding a detail I left out — the estimate assumes we keep the current provider.",
	"Worth flagging that this touches the same code as the export work.",
	"I put the working notes in the shared drive rather than pasting them here.",
	"The other option costs about the same and takes a week longer.",
	"For context, this is the second time the same question has come up this month.",
	"I have a preference but it is weak, so I will go with whatever you say.",
	"Two people have looked at this and neither found anything else.",
	"There is a smaller version of this we could do first.",
	"Nothing here is urgent; I wanted it written down before it got forgotten.",
}

// counterpartCloses end a thread without leaving anything open.
var counterpartCloses = []string{
	"Thanks — closing this out.",
	"Got it.",
	"Understood, thanks.",
	"Perfect. Done.",
	"That was all I wanted to check.",
	"Copy that.",
	"Noted — thanks.",
	"Clear, thank you.",
	"Appreciated.",
	"Right, that settles it.",
	"Thanks for the quick turnaround.",
	"Done on my side.",
}
