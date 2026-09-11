# CG-06 Trusted Delivery Probe

This file is a disposable, non-runtime probe used to verify that the default-branch trusted delivery workflow evaluates the exact pull-request head SHA and publishes the `engineering-governance/trusted-delivery` status from the GitHub Actions App.

It does not change application code, dependencies, Yunka, generated code, `server/**`, or workflow definitions.
