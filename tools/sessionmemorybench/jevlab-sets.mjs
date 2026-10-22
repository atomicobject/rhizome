// Question sets for the Jev lab. Screening is an early recall filter: it
// removes episodes no extraction model would learn from, and leaves the
// subtle "is this worth keeping" judgment to the model that reads what
// survives. Questions therefore ask about observable features of a message.
//
// Each question may name the lenses it applies to; it is asked of every
// episode otherwise.

import { SCREEN_QUESTIONS } from "./prompts.mjs";

export const SETS = {
  // The screening questions the benchmark uses today: one per lens.
  v1: Object.fromEntries(Object.entries(SCREEN_QUESTIONS).map(([lens, q]) => [`v1_${lens}`, { lenses: [lens], instructions: q }])),

  // Narrow, observable signals asked of every episode.
  surface: {
    pushback: {
      instructions: "Does a person push back on, correct, or redirect what the agent did or said, or tell it how something should be done?",
    },
    cause: {
      instructions: "Does the message explain why something failed, broke, or behaved unexpectedly?",
    },
    specifics: {
      instructions: "Does the message name specific files, commands, flags, functions, settings, or tools of the project being worked on?",
    },
    surprise: {
      instructions: "Does the message report that something turned out different from what was expected or assumed?",
    },
    location: {
      instructions: "Does the message say where something lives in the project, or which code path or component actually handles something?",
    },
    constraint: {
      instructions: "Does the message state a requirement, restriction, or rule about how work must be done in this project, such as a command that must be used or a step that must not be skipped?",
    },
    status_only: {
      instructions: "Is the message only a progress update, a plan, or a summary of edits made and tests run, with nothing about how the project itself works?",
    },
  },

  // Passage questions: each paragraph or list item of a message is asked
  // separately, and an episode scores as its best passage.
  passage: {
    p_reusable: {
      passages: true,
      instructions: "Does this passage state something about the project that a later engineer could reuse: how some part works, why something failed or behaved unexpectedly, a requirement or rule for doing work there, or where something lives? Answer no when it only reports what was done, a plan, a status, or test results.",
    },
    p_how: {
      passages: true,
      instructions: "Does this passage say anything about how some part of the project works, behaves, or must be used, even while describing a change that was made?",
    },
    p_surprise: {
      passages: true,
      instructions: "Does this passage report that something in the project turned out different from what was expected, or name the root cause of a problem?",
    },
  },

  // One permissive question: keep anything with any content about the
  // project, and drop only messages that clearly carry none.
  permissive: {
    informative: {
      instructions: "Could a careful engineer learn anything at all about how this project works, how its tools behave, or how its people want work done from this message, even something small?",
    },
    empty: {
      instructions: "Is this message clearly empty of information about the project, such as a greeting, an acknowledgement, an approval, a request for the next step, a question, or a bare status report?",
    },
  },

  // The permissive question with its boundary spelled out.
  permissive_criteria: {
    informative_c: {
      instructions: "Does this message contain any information about how this project works, how its tools behave, or how its people want work done?",
      criteria: {
        true: {
          what: "The message states something about the project that a later reader could reuse, however small, even inside a summary of work.",
          examples: [
            "The tests failed because the fixture vault was not registered; register it once with rzm vault add.",
            "No, don't use npx prettier here; run npm run format.",
            "The search ranking lives in pkg/search/rank.go, not in the web client.",
            "Done. Note that make build skips the web UI when NO_WEB=1 is set.",
          ],
        },
        false: {
          what: "The message only acknowledges, approves, asks, plans, or reports progress, with nothing reusable about the project.",
          examples: ["Sounds good, go ahead.", "I'll start by reading the file.", "All 14 tests pass and the change is committed.", "Can you run it again?"],
        },
      },
    },
  },
};
