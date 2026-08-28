# Qniform OnRamp

## CLAUDE instructions

This is a summary of the project. Don't write any code yet, I want to discuss and agree the design and architecture in some detail before we start.

## Intro

OnRamp is a program for migrating from one books and records system to Qniform (~/Code/qniform6) for large financial firms such as brokers and banks. This is a long running process which can take months or even years.

This document will describe the business domain, the functionality, and to a degree the technology stack of the system

The basis of the migration is extracted data from the source (legacy) books and records system, as well as various anciallary systems the migrator firm has. Typically these will be in the form of CSVs extracted from the source system, but may also take the form of flat files.

The process of the migration involves starting with a core set of these datasets, and, attached to a running instance of Qniform, gradually over a period of months deriving a set of transformation rules that map the source data to the target system. The broad loop will be: a) get a new set of data from the client b) define a schema for that dataset c) create transformation rules which incorporate that source into the transformation rules, and load the data into Qniform d) After the load, run a regression test suite against that instance of Qniform, write new ones, and otherwise inpect the system to see if the data landed properly, including discussions with the client e) return to b to iteratively change the schema, rules, tests etc.

## Source Schemas and Datasets

We are mainly aiming at 3 or 4 big legacy systems - mainframe systems written in COBOL - as the origin of the data. These tend to have semi standardized data/table schemas. So the idea will be that, when you start a new migration project, you choose the system you are migrating from and get the out-of-the-box schemas that we already know about for those systems. However, even with those systems, there is variation in the datasets, and particularly in how they are used. And there are always datasets from OTHER systems, which might be bespoke to that client. So it's a mix of standardized, semi-standardized, and novel schemas that we are dealing with.

Some domain language I'd like to use:

- Migration Type: The name of one of the big 4 systems we're migrating from. The project will be keyed on this, and it will drive the base schemas that are available, but that isn't to say that we'll ONLY be getting data from this system.
- Source System: The name of any system which we can get data from
- Source Schema: A named 'table' of data from a given source system - including row-by-row names, types and descriptions, and an indication of whether the field is required or not
- Dataset: Keyed to a Source Schema, a particular dataset that is uploaded. The dataset is validated againts the Source Schema

The user should be able to see and change the schemas, upload new datasets associated with those schemas, and view those uploaded datasets.

## Targets and transformation rules

The overall goal is to take the source system data and transform it into something that is understood by Qniform - in particular the Qniform APIs (OpenAPI spec is available in that repo).

To do this, we need to transform the pretty much exclusively flat (and mostly string typed) data into the mostly flat, but in some cases not flat API structure of Qniform.

It should be noted that, while the data is conceptually similar in the source and target schemas (accounts, securities, transactions), in detail they are quite different. It is certainly the case that an API call for Qniform will source from many different source datasets, and will need to derive data based on inputs from several source datasets.

The tweaking of the transformation rules, and the observation of the results of those tweaks, is the core loop of the system. It needs to be fast, observable, and feel good. This probably means that there needs to be some 'in system' observation mechanism in the onramp system itself - partly because the connection between source and results is the key insight the user needs, and partly because the API is probably the performance bottleneck.

## Runs as deterministic functions and auditability

These migrations take place over many months and have many people working on them concurrently. It's important that changes are recorded, and results reproducable, so that a change made 6 months ago (and WHY that change was made) is not lost.

To effect this, we rely on the 'pure-function' analogy:

```
Output = Apply(SourceSchemas, SourceDatasets, Xforms)
```

By tracking the versions of each of these three inputs, we guarantee deterministic and repeatable output. For example, the output from applying Xforms V3 to SourceSchemas V2 and SourceDatasets V5 will always be the same. It's essential that all these changes are audited, recording who made them, when and critically why.

To capture the progression of outputs, we create the 'Run' concept: A Run captures the versions of all the inputs, and various metadata such as the run number, the datetime of the run, and the user who completed the run. A project is therefore a sequence of Runs.

## Knowledge base and knowledge evolution

A key characteristic of the large, old systems that we are migrating from is that neither we, nor the client, nor probably even the vendor, knows everything about how the system works, and the various, often subtle, implications of the various fields and switches. A migration is therefore as much about gradually increasing our knowledge about how the source system works, and how that relates to equivalent functionality in Qniform. That knowledge about how the source system works and maps - beyond the 'this field from the source maps to this field in the target' - needs to be captured and made available to the entire onramp userbase, and carry over from project to project.

## AI Assisted Migrations

Defining and maintaining transformations of the type we will be using for this often means, in effect, reading and writing code in a transformation language like SQL or JQ. This puts a floor on the type of person who can use the system, because they will need to know how to do that. In other words, you need a data engineer. And often, under the high pressures of a live migration, people are incentivized to prioritize short-term hack fixes to transformation rules, leaving technical debt which over time slows down the migration.

The proposal for this product is to use an AI "Data Engineer in a Box" who can help a non-technical user understand the entire flow of a run, from the source data itself, to the implications of changing schema and transformation rules. For example, if you change the type of a source schema column, you probably need to update a transformation rule in sympathy with that. An appropriately prompted AI assistant can trace through the implications of making changes to the rules and source schemas in a consistent way, and give the user sufficient context so that they can understand the impact of the changes they are making (by giving examples).

Further, the knowledge base idea described above comes naturally in to this: we often have large volumes of very tedious documentation for these legacy systems. If we can find a way to make these accessible to the AI, as well as train the AI through exposing it to domain experts (for example, using a RAG), then the AI could become useful not only in assisting with the technical aspects of managing transformations, but also with the _business_ aspects of making judgements about how to map things, writing regression tests, and so on.

## Milestones and Outcomes

These projects take a long time even in the base case, and also have a tendency to 'drift' - doubling the initial estimated time to implement is not unheard of.

What we want to do with this product is to tie the 'data side' of onramp to the 'project side'. The system will divide the migration project into several "Milestones" - maybe 4 or 5 per project, each one probably taking between 3 weeks and 2 months. The milestones themselves consist of several "outcomes": concrete things we want to achieve during the milestone, stated in terms of expected behavior of the qniform system. "Equity Trades can create trade confirms, but not when the customer has opted out" for example. This is where regression testing would come into the picture. And all connected together: the changes to the data transformations to map the opt-out behavior, the regression tests, all connected to the outcome in perpetuity.

## Note on volumes

The types of system we're dealing with might have very large volumes - on the order of tens of millions of accounts and hundreds of millions of transactions. This is challenging to the 'fast feedback' loop we described above, with the "transform, observe, tweak, transform" being something that should be taking seconds. There are various ways to solve this, but I want this volume assumption to be in there from the beginning.

## Technology

The backend will be a Go web application exposing an API for a react frontend.

We will use a Postgres database as required, unless there is a valid use case for using a different database technology for a specific purpose.

All other implementation decisions are open, but I would want an early decision on how the transformation rule encoding is going to work.

## Next steps

Discuss the broad architecture, technology stack, and sketch a high level API for the backend.
