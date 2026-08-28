# Domain Glossary

## Tier

Migrations are big - millions of rows. Transforming and loading this much data can take hours. We need something smaller.

When deciding what to transform, we can choose a _tier_ of tranformation. A "Full" tier is the full monty: all the data. A "Sample" tier run is a 1% selection of input data. A "Slice" is a sampling of ~1k rows.

A tier is a string on a `Run` and a `ApplyInput`.

## Actor

Kind and ID string - user?

## SourceSchema

A named table from a Source System. Lots of string types

- source system
- Schema
- Title
- Description
- Layout
- Header (Bool)
- Fields `[]Field`

A Field has the following

- Name
- Type (_logical_ type - string int, dec, date, bool, enum
- required bool
- Description
- Encoding (A kind - trim, cyymmdd, overpunch_trailing, yn, packed-dec)
- Scale (???)
- Values (str->str) (??? is this for enums ???) 
- Null Values []str

## Dataset

Uploaded body of data. Has a name, schema-ref (is this a hash?), hash (of the dataset itself), raw_path (is this necessary?) Rows (should be row count?) Rejected (int, should be rejected count), StagePath (link to the Parquet where the data is stored)

## Xform

Description of a transformation. Xforms operate on the parquet.

Name, Version (int? why doesn't everything else have an int version? inconsistent?), Description.

- Inputs: list of source schemas this xform operates on (what is an As?).
- Entities: list of entities (Account etc.) ???
- Output: the concrete list of entities being output (an API call?)
- Rules: 

(inputs->schemas?, entities - not domain term? Output types? Mismatch between inputs and outputs)

Output: Name, Title, Entity, Emitter, Target (???), After (???)

## Spec

Qniform's OpenAPI document. Emitted batches can be cheaply checked against this before actually hitting the endpoint.

## Engine

## Core transformation

- CSV -> Parquet (is this the 'staging?')
- Parquet -> 

## Misc:

Layout in source-schema: csv/fixed. Superfluous.
