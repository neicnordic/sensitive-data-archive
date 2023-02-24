package no.uio.ifi.localega.doa.rest;

import lombok.extern.slf4j.Slf4j;
import no.uio.ifi.localega.doa.aspects.AAIAspect;
import no.uio.ifi.localega.doa.services.MetadataService;
import no.uio.ifi.localega.doa.services.StreamingService;
import org.apache.commons.lang3.StringUtils;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.core.io.InputStreamResource;
import org.springframework.http.*;
import org.springframework.web.bind.annotation.*;

import javax.security.auth.message.AuthException;
import javax.servlet.http.HttpServletRequest;
import java.io.InputStream;
import java.util.Set;

/**
 * REST controller incorporating streaming-related endpoints.
 */
@Slf4j
@ConditionalOnProperty("rest.enabled")
@RequestMapping("/files")
@RestController
public class StreamingController {

    @Autowired
    protected HttpServletRequest request;

    @Autowired
    protected MetadataService metadataService;

    @Autowired
    protected StreamingService streamingService;

    /**
     * Streams the requested file.
     *
     * @param publicKey         Optional public key, if the re-encryption was requested.
     * @param fileId            ID of the file to stream.
     * @param destinationFormat Destination format.
     * @param startCoordinate   Start byte.
     * @param endCoordinate     End byte.
     * @return File-stream.
     * @throws Exception In case of some error.
     */
    @SuppressWarnings("unchecked")
    @GetMapping("/{fileId}")
    public ResponseEntity<?> files(@RequestHeader(value = "Public-Key", required = false) String publicKey,
                                   @PathVariable(value = "fileId") String fileId,
                                   @RequestParam(value = "destinationFormat", required = false) String destinationFormat,
                                   @RequestParam(value = "startCoordinate", required = false) String startCoordinate,
                                   @RequestParam(value = "endCoordinate", required = false) String endCoordinate) throws Exception {
        try {
            Set<String> datasetIds = (Set<String>) request.getAttribute(AAIAspect.DATASETS);
            InputStream inputStream = streamingService.stream(datasetIds, publicKey, fileId, destinationFormat, startCoordinate, endCoordinate);
            return ResponseEntity.ok().headers(getResponseHeaders(fileId, StringUtils.isNotEmpty(publicKey))).body(new InputStreamResource(inputStream));
        } catch (AuthException e) {
            log.info("User doesn't have permissions to download requested file: {}", fileId);
            return ResponseEntity.status(HttpStatus.FORBIDDEN).build();
        }
    }

    private HttpHeaders getResponseHeaders(String fileId, boolean encrypted) {
        String fileName = metadataService.getFileName(fileId);
        HttpHeaders responseHeaders = new HttpHeaders();
        responseHeaders.setContentType(MediaType.APPLICATION_OCTET_STREAM);
        if (encrypted) {
            fileName += ".enc";
        }
        responseHeaders.setContentDisposition(ContentDisposition.builder("attachment").filename(fileName).build());
        return responseHeaders;
    }

}
