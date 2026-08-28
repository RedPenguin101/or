# Core Model

The purpose of the program is to migrate a set of data extracts representing the books and records of a legacy system (the **Source** into Qniform (the **Target**). This is done by means of the **Transformations**.

There are 4 key things that factor into this:

A **Source System** is a 3rd party system which contains data which we want to import into Qniform.

A **Source Schema** describes a data table from a Source System. It has a list of **Schema Fields** - a description of columns in the table layout, including information about how to parse the values.

A **Source Dataset** is a concrete set of data extracted from the source system. The system, when given a csv file and told which Source Schema, will create the Dataset, coercing the data from the file as it goes.

A **Target Entity** is a type of object in Qniform which is ultimately created by the onramp. Examples are Accounts, Parties, EquityTrades. A Target Entity is described by its Target Spec, ultimately encoded as the OpenAPI spec of the relevant Qniform endpoint.

A **Transformation Group** is a set of rules which converts between Source and Target. A TG is linked to the set of Source Schemas that it uses, and a set of Target Entities it creates. A TG application has two stages: **Rules** written in SQL which transform Source Datasets into flat representations of the target entities. An **API Formatter** handles the final mile of transforming these flat datasets into the nested JSON that gets uploaded into Qniform.

Within the context of a project, each of these things (except the Source System) is **uniquely versioned**. 

A **Run** is an application of a given collection of Transformation Groups to a given set of Source Datasets conforming to a given set of Source Schemas, producing **Output Results** - the JSON payloads which will ultimately be uploaded to Qniform.

```
ApplyRun(TransformationGroups, SourceDatasets, SourceSchemas) -> TargetOutputs
```

Because each of the inputs to this are versioned, the Target Output for a given triple of input versions is deterministic.

## Concepts I don't understand

### Run Nodes

### Run Node Stats

### Run Target

### Model

### Views

### Bind / MapBind in Emitter

### Field Order 

why does the order of the fields matter at all? These are just maps. If we care about the order for display, that's a different concern than the emissions (which are just for getting the data shape, not order dependent unless the target cares.).

## Ancilliary Concepts

### Engine

The thing that applies most of the transformations. 

In the pre-transformation, the `Ingest` function takes an engine, a 'store' (a path to the parquet that is the storage format), the SourceSchema and the raw input bytes. The engine `WriteStage` method is called, which creates the parquet files (the "Stage", though this probably shouldn't be a domain term). A Source Dataset is returned.

Then in the Run itself, the engine is used again to load the Dataset and apply the Rules of the Transformation Group.

The final stage, the API Formatter, is not handled by the engine.

### Emitter

The thing that manages the final stage of the transformation - the binding from the flat output of the engine to the nested JSON / nested output that is required by the Qniform API.

#### Row Key

#### Batch

Batch is a pretty anemic wrapper around MaxItems int and failure mode string (per-item, and all-or-nothing modes). I don't know if this is the correct place for this - surely this should be in the _transformation_. The Emitter final stage should be pretty much deterministic: if the Rule transform worked, it should pretty much be guaranteed that the Emitter does.


## Things to delete / rename

TODO: Write the same Xrefs tool that tracks cross-file calls

Tiers/Actors: Premature to have this

OutputResult is not a great name - Target Outputs?

Apply seems like a spare concept: It takes an ApplyInput and produces a Run. Surely this should just be `DoRun :: RunInput -> RunOutput`

Though RunInput is ALREADY a struct - Schemas, Datasets, Xform, Triple. ApplyInput is a superset of this.

Stage concept still exists, shouldn't

The ideal of a 'Model' - isn't this just the transformation rules?

Emitter I don't love. API Formatter? Maybe undersells what it does.

"Prior" in ApplyInput is not a great name

Xform -> Transform, no need to abbreviate until it hurts

SchemaRef - obscure, this is presumably the hash of the SourceSchema?

The "Quarantine" term needs clarifying: this seems to be things that are couldn't be cooerced to the SourceSchema. Fine, but consider the wider flow - how are they resolved? In the App? If not, why allow it to proceed?

Target entities should be more reified in onramp than they are. The Targets, unlike the Sources are quite fixed to the User.

Source (in run) is an odd name for 'how much data the Run actually read' - especially in combinations wit Totals in the same struct.

## Simplified Model

- SourceSystem: Enum, FAS only member currently
- SourceSchema: SourceSystems, SchemaFields
  - SchemaField: basically as it is now (not sure what "Scale" is though)
- Datasets: as now, references the SourceSchema
- Transforms
  - SourceSchemas
  - TargetSpecs
  - Rules
	- FlatRule
	- APIFormatter
- RunInputs
  - Datasets
  - Transforms
- RunOutputs
  - RunInputs
  - 

## Reviewed files

```
api.go
app.go
dataset_api.go
db.go
encoding.go
main.go
openapi.go
output_api.go
project_api.go
reject.go
run_api.go
schema.go
schema_api.go
xform.go
```
