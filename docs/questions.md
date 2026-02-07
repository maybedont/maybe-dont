## Questions

* What code can we remove that is just there for backwards compat if we are going to release version 1.0?
* We "ship" rules, but for docker they won't be included currently (I don't think)
    * Should we embed them so we always have defaults? 
    * Or keep the way they are packaged, but modify the docker build to pick them up and use a "default_rules" directory in the docker image and copy them in.  The problem with this is that the rules are in the same dir as the config currently, and we can't really include a default config file because it contains the MCP servers. We could make a rules directory sepearate from the config but default it to the same as the config and then ship default rules in the docker image? Shipping rules in the docker image isn't ideal in some sense.
* How is the installationId used, how do we persist it for binary downloads, and docker usage.
* Consider making the MCP server an 'mcp' sub command on the binary CLI
* Website should focus less on MCP and more on the problem statement since MCP is only part. WE can make landing pages for CLI and MCP.
* Policies are binary, and each policy has to have boiler plate on how to respond. This should go into the rujntime code so we can append this consistently instead of making each policy do it.
* How can better decide which policies to run if we know the tool or cli command is read only such as a get, search, etc? In those case, the response validation for deny or redact can make sense.
    * COuld we ask AI to classify the call with a confidence score if this is a read-only operation? Or do we classify this ourselves?
* For tools or cli that mutate - such as POST, PUT, DELETE equivalents in tool and cli calls, deny doesn't make sense on response validation* Should we read the meta data on MCP tools for read-only and have that help us understand the tool call? Or should we ignore this as un-trustowrthy.