## Questions

* Are we sure we can't use keyed ENVIRONMENT or config properties to configure MCP servers? If so, we can make the config file optional
* What code can we remove that is just there for backwards compat if we are going to release version 1.0?
* We "ship" rules, but for docker they won't be included currently (I don't think)
    * Should we embed them so we always have defaults? 
    * Or keep the way they are packaged, but modify the docker build to pick them up and use a "default_rules" directory in the docker image and copy them in.  The problem with this is that the rules are in the same dir as the config currently, and we can't really include a default config file because it contains the MCP servers. We could make a rules directory sepearate from the config but default it to the same as the config and then ship default rules in the docker image? Shipping rules in the docker image isn't ideal in some sense.
* How is the installationId used, how do we persist it for binary downloads, and docker usage.
* Should we use a YAML array instead of a map for the downstream MCP servers?
* Should we rename cel_engine to rules_engine for consistency? 