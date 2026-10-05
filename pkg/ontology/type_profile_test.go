package ontology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const profileSDL = `
enum TaskStatus {
  todo @view(stage: "open")
  doing @view(stage: "active")
  done @view(stage: "done")
}

enum SpecStatus {
  proposed @view(stage: "open")
  accepted @view(stage: "done")
  retired @view(stage: "dropped")
}

enum Priority {
  high @view(tone: "warning")
  low
}

enum Topic {
  design
  research
}

enum IdeaStatus {
  raw
  exploring @view(tone: "progress")
  parked @view(collapsed: true)
}

type Person @node(paths: ["people/*.md"]) {
  name: String
}

type Team @node(paths: ["teams/*.md"]) {
  name: String
}

type Task @node(paths: ["tasks/*.md"]) {
  id: String @identifier(preferred: true)
  summary: String
  priority: Priority
  status: TaskStatus! @display(importance: KEY)
  goal: String @display(importance: KEY)
  owner: Person @link
  team: Team @link
  blocker: Task @display(importance: KEY) @link
  due: Date
  dependents: [Task!] @reverse(field: "blocker")
  mentions: [Spec!] @neighbors(direction: INBOUND, type: "Spec")
}

type Spec @node(paths: ["specs/*.md"]) {
  pitch: String @display(role: SUMMARY)
  status: SpecStatus!
  updated: Date
}

type Meeting @node(paths: ["meetings/*.md"]) {
  held: DateTime
  reviewed: Date @display(importance: DETAIL)
  topic: Topic
}

type Doc @node(paths: ["docs/*.md"]) {
  topic: Topic
  archived: Boolean! @display(importance: KEY)
}

type Page @node(paths: ["pages/*.md"]) {
  title: String @display(importance: KEY)
  written: Date
  edited: Date
}

type Idea @node(paths: ["ideas/*.md"]) {
  stage: IdeaStatus @display(importance: KEY)
  mood: IdeaStatus
}

type Hunch @node(paths: ["hunches/*.md"]) {
  stage: IdeaStatus
}

type Release @node(paths: ["releases/*.md"]) @requiresWhen(field: "state", equals: "shipped", require: [{field: "shippedOn"}]) {
  state: ReleaseState! @display(importance: KEY)
  version: String! @display(importance: KEY)
  shippedOn: Date @display(importance: KEY)
  notes: String @display(importance: KEY)
}

enum ReleaseState {
  planned
  shipped
}

interface Work {
  status: TaskStatus @display(importance: KEY)
  lead: Person @link
}

type Chore implements Work @node(paths: ["chores/*.md"]) {
  status: TaskStatus @display(importance: KEY)
  lead: Person @link
  room: String
}
`

func TestDeriveTypeProfile(t *testing.T) {
	schema := mustLoadSDL(t, profileSDL)
	derive := func(name string) TypeProfile {
		t.Helper()
		profile, ok := DeriveTypeProfile(schema, name, "Person")
		require.True(t, ok, name)
		return profile
	}

	require.Equal(t, TypeProfile{
		Shape:          ShapeWorkflow,
		LifecycleField: "status",
		OrderedFields:  []string{"priority"},
		SummaryField:   "summary",
		PeopleFields:   []string{"owner"},
		KeyTextFields:  []string{"goal"},
		RelationFields: []string{"blocker", "team"},
		ReverseFields:  []string{"dependents", "mentions"},
		GapFields:      []string{"goal", "blocker"},
	}, derive("Task"), "a non-KEY date is not primary when a lifecycle exists")

	require.Equal(t, TypeProfile{
		Shape:          ShapeContract,
		LifecycleField: "status",
		SummaryField:   "pitch",
	}, derive("Spec"), "a declared-stage enum without active is a contract; stages need not be KEY")

	require.Equal(t, TypeProfile{
		Shape:            ShapeDated,
		CategoryFields:   []string{"topic"},
		PrimaryDateField: "held",
	}, derive("Meeting"), "the only non-DETAIL date is primary without a lifecycle")

	require.Equal(t, TypeProfile{
		Shape:          ShapeCatalog,
		CategoryFields: []string{"topic"},
	}, derive("Doc"), "a KEY Boolean is never a lifecycle")

	require.Equal(t, TypeProfile{Shape: ShapeReference, GapFields: []string{"title"}}, derive("Page"),
		"two plain dates name no primary date, and title is not key text")

	require.Equal(t, TypeProfile{
		Shape:          ShapeWorkflow,
		LifecycleField: "stage",
		OrderedFields:  []string{"mood"},
	}, derive("Idea"), "a KEY enum with inferred stages is the lifecycle")

	require.Equal(t, TypeProfile{
		Shape:         ShapeReference,
		OrderedFields: []string{"stage"},
	}, derive("Hunch"), "a non-KEY inferred enum is ordered, not a lifecycle")

	require.Equal(t, TypeProfile{
		Shape:          ShapeWorkflow,
		LifecycleField: "status",
		PeopleFields:   []string{"lead"},
	}, derive("Work"), "an interface uses its own fields")

	require.Equal(t, TypeProfile{
		Shape:            ShapeDated,
		CategoryFields:   []string{"state"},
		PrimaryDateField: "shippedOn",
		KeyTextFields:    []string{"version", "notes"},
		GapFields:        []string{"notes"},
	}, derive("Release"), "required and @requiresWhen-conditional KEY fields are not gaps")

	_, ok := DeriveTypeProfile(schema, "Missing", "Person")
	require.False(t, ok)
}

func TestInterfaceGapFieldsFollowWhatEveryImplementorRequires(t *testing.T) {
	schema := mustLoadSDL(t, `
enum Kind { a b }
interface Doc {
  kind: Kind @display(importance: KEY)
  reason: String @display(importance: KEY)
  owner: String @display(importance: KEY)
  note: String @display(importance: KEY)
}
type Memo implements Doc @node(paths: ["memos/*.md"]) @requiresWhen(field: "kind", equals: "b", require: [{field: "reason"}]) {
  kind: Kind @display(importance: KEY)
  reason: String @display(importance: KEY)
  owner: String! @display(importance: KEY)
  note: String! @display(importance: KEY)
}
type Brief implements Doc @node(paths: ["briefs/*.md"]) {
  kind: Kind @display(importance: KEY)
  reason: String! @display(importance: KEY)
  owner: String! @display(importance: KEY)
  note: String @display(importance: KEY)
}
`)
	profile, ok := DeriveTypeProfile(schema, "Doc", "Person")
	require.True(t, ok)
	require.Equal(t, []string{"kind", "note"}, profile.GapFields,
		"reason is required or conditionally required by every implementor, owner required by both, note by only one")
}
