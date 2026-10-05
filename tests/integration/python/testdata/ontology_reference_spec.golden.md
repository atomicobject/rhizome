# Ontology Reference

## Spec

- label: Spec
- role: NOTE
- propertyCase: KEBAB
- paths: notes/specs/*.md
- annotations: @node {"default":false,"locator":"FILE","paths":["notes/specs/*.md"],"propertyCase":"KEBAB"}
- view profile: reference; summary summary

### Fields

- name!: String [scalar]
  source: name [FRONTMATTER]
  scope: NOTE
- requirements!: RequirementsSection [section]
  scope: NOTE
  binding: heading-derived subtree
  heading: Requirements
  level: H2
  matching: exact heading text + exact level
  section required: true
  annotations: @contains {"heading":"Requirements","level":"H2","required":true}
- stories: StoriesSection [section]
  scope: NOTE
  binding: heading-derived subtree
  heading: Stories
  level: H2
  matching: exact heading text + exact level
  annotations: @contains {"heading":"Stories","level":"H2","required":false}
- summary: String [scalar]
  source: summary [FRONTMATTER]
  scope: NOTE
  display: role SUMMARY
