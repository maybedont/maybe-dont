# Developer ReadME

We should start automating this, but for now here are a few things you may need to know if you're a new developer here. 

## Requirements

You'll need to ensure you have some tools configured. 

- `go` (duh)
- `golangci-lint` - for linting
- `goreleaser` for running `make snapshot`
- `cz` for running `make bump-version`

## Shell configuration

`.zshrc` or similar

```
export METRICS_DATASET=maybedont_dev_dataset_name
export METRICS_API_TOKEN=maybedont_dev_dataset_api_token
```

## Testing

### Docker
Run `./scripts/buildLocalDockerImage.sh` to build a local docker image for testing. This command will produce an image named `local/maybe-dont:dev`. You can run this image by calling `docker run local/maybe-dont:dev`.

## Releasing

The GHA runner will run `.github/workflows/releaser.yaml` on tag creation. To trigger a release, run the following command:

> make bump-version

## Troubleshooting

### `make bump-version`

1. If this command fails, ensure you have installed and configured `gpg` for use with GitHub. 
2. Ensure you have set `GPG_TTY`

    Example:
    
    > export GPG_TTY=$(tty)