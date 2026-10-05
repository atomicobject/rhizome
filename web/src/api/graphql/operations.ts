export const PUBLIC_NODE_DETAIL_QUERY = /* GraphQL */ `
  query PublicNodeDetail($ref: String!, $nodeLimit: Int = 50, $edgeLimit: Int = 100) {
    node(ref: $ref) {
      ref {
        ref
        kind
        notePath
        path
        fragment
        nodeId
        structural
        typeName
      }
      nodeId
      nodeKind
      path
      title
      resolvedType
      locator {
        ref {
          ref
          kind
          notePath
          path
          fragment
          nodeId
          structural
          typeName
        }
        kind
        sourceLocator
        status
        wikilink
        markdown
        exists
        requiresFix
        linkTarget {
          ref {
            ref
            kind
            notePath
            path
            fragment
            nodeId
            structural
            typeName
          }
          markdown
          wikilink
          displayLabel
          exists
          requiresFix
          blockId
        }
        diagnostics {
          code
          notePath
          ref {
            ref
            kind
            notePath
            path
            fragment
            nodeId
            structural
            typeName
          }
          blockId
          message
        }
        fixActions {
          ref {
            ref
            kind
            notePath
            path
            fragment
            nodeId
            structural
            typeName
          }
          blockId
        }
      }
      workspace {
        parentRef {
          ref
          kind
          notePath
          path
          fragment
          nodeId
          structural
          typeName
        }
        parentTitle
        sourceLinks(first: 100) {
          target
          authoredTarget
          text
          kind
          anchor
          embed
          targetKind
          resolved
          resolvedRef {
            ref
            kind
            notePath
            path
            fragment
            nodeId
            structural
            typeName
          }
          title
          preview
        }
        fields {
          name
          kind
          sourceKind
          present
          status {
            dirty
            validation {
              issueCount
            }
            freshness {
              state
            }
            session {
              state
            }
            hasWarnings
          }
          range {
            start
            end
          }
          valueRanges {
            start
            end
          }
          values
          links {
            value
            resolved
            title
            ref {
              ref
              kind
              notePath
              path
              fragment
              nodeId
              structural
              typeName
            }
          }
          sectionNodes {
            ref
            kind
            notePath
            path
            fragment
            nodeId
            structural
            typeName
          }
          capability {
            ownerRef {
              ref
              kind
              notePath
              path
              fragment
              nodeId
              structural
              typeName
            }
            ownerType
            typeName
            valueKind
            list
            required
            enumValues
            enumOptions {
              value
              label
              tone
            }
            targetType
            sourceKind
            valueOrigin
            identifier
            preferredIdentifier
            displayImportance
            writeOperation
            readOnlyReason
          }
          issues {
            code
            notePath
            typeName
            fieldName
            nodeRef
            nodeId
            line
            message
          }
        }
        collections {
          name
          kind
          status {
            dirty
            validation {
              issueCount
            }
            freshness {
              state
            }
            session {
              state
            }
            hasWarnings
          }
          range {
            start
            end
          }
          items {
            ref {
              ref
              kind
              notePath
              path
              fragment
              nodeId
              structural
              typeName
            }
            range {
              start
              end
            }
          }
          orderFingerprint
        }
        bodies {
          ref {
            ref
            kind
            notePath
            path
            fragment
            nodeId
            structural
            typeName
          }
          title
          resolvedType
          locator
          level
          blockId
          parentRef {
            ref
            kind
            notePath
            path
            fragment
            nodeId
            structural
            typeName
          }
          markdown
          binding {
            typeName
            fieldName
            fieldPath
            fieldList
            sectionDisplay
            properties
            identifierField
            previewTemplate
            collapsed
          }
          fields {
            name
            kind
            sourceKind
            present
            status {
              dirty
              validation {
                issueCount
              }
              freshness {
                state
              }
              session {
                state
              }
              hasWarnings
            }
            range {
              start
              end
            }
            valueRanges {
              start
              end
            }
            values
            links {
              value
              resolved
              title
              ref {
                ref
                kind
                notePath
                path
                fragment
                nodeId
                structural
                typeName
              }
            }
            sectionNodes {
              ref
              kind
              notePath
              path
              fragment
              nodeId
              structural
              typeName
            }
            capability {
              ownerRef {
                ref
                kind
                notePath
                path
                fragment
                nodeId
                structural
                typeName
              }
              ownerType
              typeName
              valueKind
              list
              required
              enumValues
              enumOptions {
                value
                label
                tone
              }
              targetType
              sourceKind
              valueOrigin
              identifier
              preferredIdentifier
              displayImportance
              writeOperation
              readOnlyReason
            }
            issues {
              code
              notePath
              typeName
              fieldName
              nodeRef
              nodeId
              line
              message
            }
          }
          collections {
            name
            kind
            status {
              dirty
              validation {
                issueCount
              }
              freshness {
                state
              }
              session {
                state
              }
              hasWarnings
            }
            range {
              start
              end
            }
            items {
              ref {
                ref
                kind
                notePath
                path
                fragment
                nodeId
                structural
                typeName
              }
              range {
                start
                end
              }
            }
            orderFingerprint
          }
          blocks {
            kind
            range {
              start
              end
            }
            markdown
            fieldName
            rawKey
            childRef {
              ref
              kind
              notePath
              path
              fragment
              nodeId
              structural
              typeName
            }
            childRefs {
              ref
              kind
              notePath
              path
              fragment
              nodeId
              structural
              typeName
            }
            sectionDisplay
          }
        }
        capabilities {
          canEdit
          canEditFields
          canEditCollections
          canNavigateChildren
          canSubscribe
        }
        status {
          dirty
          validation {
            issueCount
          }
          freshness {
            state
          }
          session {
            state
          }
          hasWarnings
        }
        version
        sourceRevision {
          notePath
          contentFingerprint
          content
        }
        assessment {
          notePath
          declaredType
          resolvedType
          candidateTypes
          issues {
            code
            notePath
            typeName
            fieldName
            nodeRef
            nodeId
            line
            message
          }
          fields {
            name
            description
            kind
            typeName
            required
            list
            source
            sourceKind
            present
            values
            validValues
            issues {
              code
              notePath
              typeName
              fieldName
              nodeRef
              nodeId
              line
              message
            }
          }
          relations {
            name
            description
            kind
            typeName
            required
            list
            source
            sourceKind
            direction
            present
            values
            targets {
              path
              typeName
              provenance
              structural
            }
            issues {
              code
              notePath
              typeName
              fieldName
              nodeRef
              nodeId
              line
              message
            }
          }
        }
        structure {
          ref {
            ref
            kind
            notePath
            path
            fragment
            nodeId
            structural
            typeName
          }
          parentRef {
            ref
            kind
            notePath
            path
            fragment
            nodeId
            structural
            typeName
          }
          title
          level
          content
        }
        relationGroups {
          key
          label
          ownerTitle
          navigation
          items {
            ref {
              ref
              kind
              notePath
              path
              fragment
              nodeId
              structural
              typeName
            }
            title
            targetTitle
            resolvedType
            relationName
            provenance
            structural
            current
          }
        }
        loaded {
          rendered
          assessment
          structure
          relations
        }
      }
      localGraph(nodeLimit: $nodeLimit, edgeLimit: $edgeLimit) {
        nodes {
          id
          nodeKind
          title
          typeName
          path
          notePath
          nodeId
          parentId
          sourceLocator
          ref {
            ref
            kind
            notePath
            path
            fragment
            nodeId
            structural
            typeName
          }
        }
        edges {
          source
          target
          kind
          relation
          relationLabel
          provenance
          structural
          weight
        }
        truncated
      }
      ... on NoteNode {
        content
        format
        sourceRepresentation
        evidenceRepresentation
        sourceCapabilities
        frontmatter
        tags
      }
      ... on Section {
        content
        notePath
        level
      }
      ... on CodeFile {
        language
        symbols(first: 50) {
          nodeId
          nodeKind
          title
          path
          resolvedType
          language
          symbol
          fqn
          signature
        }
      }
      ... on CodeSymbol {
        language
        symbol
        fqn
        signature
        docComment
      }
    }
  }
`;

export const PUBLIC_LOCAL_GRAPH_QUERY = /* GraphQL */ `
  query PublicLocalGraph($ref: String!, $nodeLimit: Int = 50, $edgeLimit: Int = 100) {
    node(ref: $ref) {
      ref {
        ref
        kind
        notePath
        path
        fragment
        nodeId
        structural
        typeName
      }
      nodeId
      nodeKind
      path
      title
      resolvedType
      locator {
        sourceLocator
        status
        wikilink
        markdown
        exists
        requiresFix
        linkTarget {
          markdown
          wikilink
          displayLabel
          exists
          requiresFix
          blockId
        }
      }
      localGraph(nodeLimit: $nodeLimit, edgeLimit: $edgeLimit) {
        nodes {
          id
          ref {
            ref
            kind
            notePath
            path
            fragment
            nodeId
            structural
            typeName
          }
          nodeKind
          title
          typeName
          path
          notePath
          nodeId
          parentId
          sourceLocator
        }
        edges {
          source
          target
          kind
          relation
          relationLabel
          provenance
          structural
          weight
        }
        truncated
      }
    }
  }
`;
