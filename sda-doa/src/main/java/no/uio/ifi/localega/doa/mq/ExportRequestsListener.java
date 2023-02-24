package no.uio.ifi.localega.doa.mq;

import com.auth0.jwt.JWT;
import com.auth0.jwt.interfaces.DecodedJWT;
import com.google.gson.Gson;
import com.google.gson.JsonSyntaxException;
import io.minio.MinioClient;
import io.minio.PutObjectArgs;
import lombok.extern.slf4j.Slf4j;
import no.uio.ifi.localega.doa.dto.DestinationFormat;
import no.uio.ifi.localega.doa.dto.ExportRequest;
import no.uio.ifi.localega.doa.services.AAIService;
import no.uio.ifi.localega.doa.services.MetadataService;
import no.uio.ifi.localega.doa.services.StreamingService;
import org.apache.commons.io.FileUtils;
import org.apache.commons.lang3.StringUtils;
import org.springframework.amqp.rabbit.annotation.Queue;
import org.springframework.amqp.rabbit.annotation.RabbitListener;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.stereotype.Service;

import java.io.File;
import java.io.InputStream;
import java.util.Collection;

/**
 * RabbitMQ listener that processes incoming export requests.
 */
@Slf4j
@ConditionalOnProperty("outbox.enabled")
@Service
public class ExportRequestsListener {

    @Autowired
    private Gson gson;

    @Autowired
    private MinioClient outboxClient;

    @Autowired
    private AAIService aaiService;

    @Autowired
    private MetadataService metadataService;

    @Autowired
    private StreamingService streamingService;

    @Value("${outbox.type}")
    private String outboxType;

    @Value("${outbox.location}")
    private String outboxLocation;

    @Value("${s3.bucket}")
    private String s3Bucket;

    @RabbitListener(
            queuesToDeclare = @Queue(name = "${outbox.queue}", durable = "false", exclusive = "true", autoDelete = "true")
    )
    public void listen(String message) {
        try {
            ExportRequest exportRequest = gson.fromJson(message, ExportRequest.class);
            DecodedJWT decodedJWT = JWT.decode(exportRequest.getJwtToken());
            String user = decodedJWT.getSubject();
            log.info("Export request received from user {}: {}", user, exportRequest);
            Collection<String> datasetIds = aaiService.getDatasetIds(exportRequest.getJwtToken());
            if (StringUtils.isNotEmpty(exportRequest.getDatasetId())) {
                exportDataset(user, datasetIds, exportRequest.getDatasetId(), exportRequest.getPublicKey(), exportRequest.getStartCoordinate(), exportRequest.getEndCoordinate());
            } else if (StringUtils.isNotEmpty(exportRequest.getFileId())) {
                exportFile(user, datasetIds, exportRequest.getFileId(), exportRequest.getPublicKey(), exportRequest.getStartCoordinate(), exportRequest.getEndCoordinate());
            } else {
                throw new RuntimeException("Either Dataset ID or File ID should be specified");
            }
        } catch (JsonSyntaxException e) {
            log.error("Can't parse incoming message: " + e.getMessage());
        } catch (Exception e) {
            log.error(e.getMessage(), e);
        }
    }

    private void exportDataset(String user,
                               Collection<String> datasetIds,
                               String datasetId,
                               String publicKey,
                               String startCoordinate,
                               String endCoordinate) {
        metadataService.files(datasetId).forEach(f -> {
            try {
                exportFile(user, datasetIds, f.getFileId(), publicKey, startCoordinate, endCoordinate);
            } catch (Exception e) {
                throw new RuntimeException(e.getMessage(), e);
            }
        });
    }

    private void exportFile(String user,
                            Collection<String> datasetIds,
                            String fileId,
                            String publicKey,
                            String startCoordinate,
                            String endCoordinate) throws Exception {
        log.info("Outbox type: {}", outboxType);
        switch (outboxType) {
            case "POSIX":
                exportFilePOSIX(user, datasetIds, fileId, publicKey, startCoordinate, endCoordinate);
                break;
            case "S3":
                exportFileS3(user, datasetIds, fileId, publicKey, startCoordinate, endCoordinate);
                break;
            default:
                throw new RuntimeException("Unknown outbox type: " + outboxType);
        }
    }

    private void exportFilePOSIX(String user,
                                 Collection<String> datasetIds,
                                 String fileId,
                                 String publicKey,
                                 String startCoordinate,
                                 String endCoordinate) throws Exception {
        InputStream inputStream = streamingService.stream(datasetIds, publicKey, fileId, DestinationFormat.CRYPT4GH.toString(), startCoordinate, endCoordinate);
        String fileName = metadataService.getFileName(fileId);
        String filePath = String.format(outboxLocation, user) + fileName;
        log.info("Exporting {} to {}", fileId, filePath);
        File file = new File(filePath);
        if (file.exists()) {
            log.warn("File exists in the outbox already, skipping");
        }
        FileUtils.copyToFile(inputStream, file);
        log.info("File exported");
    }

    private void exportFileS3(String user,
                              Collection<String> datasetIds,
                              String fileId,
                              String publicKey,
                              String startCoordinate,
                              String endCoordinate) throws Exception {
        InputStream inputStream = streamingService.stream(datasetIds, publicKey, fileId, DestinationFormat.CRYPT4GH.toString(), startCoordinate, endCoordinate);
        String fileName = metadataService.getFileName(fileId);
        String filePath = user + "/" + fileName;
        log.info("Exporting {} to {}", fileId, filePath);
        outboxClient.putObject(PutObjectArgs.builder().bucket(s3Bucket).object(filePath).stream(inputStream, -1, 10485760).build());
        log.info("File exported");
    }

}
