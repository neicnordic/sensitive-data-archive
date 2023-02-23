# Known Issues and Troubleshooting

## Files metadata endpoint doesn't work with visas
If using GA4GH Visas with `/metadata/datasets/{dataset}/files`, e.g. `/metadata/datasets/https://doi.org/abc/123/files`, a reverse proxy might remove adjacent slashes `//`->`/`.
This has been observed with nginx, with a fix as follows:

[disable slash merging](http://nginx.org/en/docs/http/ngx_http_core_module.html#merge_slashes)
in `server` context
```
server {
    merge_slashes off
}
```