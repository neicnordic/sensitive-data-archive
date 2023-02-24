package se.nbis.lega.inbox.s3;

import com.amazonaws.services.s3.AmazonS3;
import com.amazonaws.services.s3.internal.Constants;
import com.amazonaws.services.s3.model.ObjectListing;
import com.amazonaws.services.s3.model.S3ObjectSummary;
import com.amazonaws.services.s3.transfer.TransferManagerBuilder;
import com.amazonaws.services.s3.transfer.Upload;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnExpression;
import org.springframework.stereotype.Service;

import java.nio.file.Path;
import java.util.Collection;
import java.util.stream.Collectors;

/**
 * Service for communicating with S3 backend.
 * Optional bean: initialized only if S3 keys are present in the context.
 */
@Slf4j
@ConditionalOnExpression("!'${inbox.s3.access-key}'.isEmpty() && !'${inbox.s3.secret-key}'.isEmpty() && !'${inbox.s3.bucket}'.isEmpty()")
@Service
public class S3Service {

    public String s3Bucket;
    private AmazonS3 amazonS3;

    /**
     * Creates a bucket if it doesn't exist yet.
     */
    public void prepareBucket() {
        try {
            if (!amazonS3.doesBucketExistV2(s3Bucket)) {
                amazonS3.createBucket(s3Bucket);
            }
        } catch (Exception e) {
            log.error(e.getMessage(), e);
        }
    }

    public Collection<String> listKeys(String userPath) {
        return amazonS3.listObjects(s3Bucket)
                .getObjectSummaries().stream()
                .map(S3ObjectSummary::getKey)
                .filter(s -> s.contains(userPath))
                .collect(Collectors.toSet());
    }

    /**
     * Uploads a file to a specified bucket synchronously.
     *
     * @param userPath Username as part of s3 object key (thus constructing the path).
     * @param key      S3 key. If not specified - obtained from path.
     * @param path     Path of the file.
     * @param sync     <code>true</code> for synchronous upload, <code>false</code> otherwise.
     */
    public void upload(String userPath, String key, Path path, boolean sync) throws InterruptedException {
        log.info("Initializing S3 upload, sync = {}", sync);
        final var userFilePath = Path.of(userPath + "/" + path);
        log.info("uploading {}, {}, {}", getKey(userFilePath), key, path.toFile());
        Upload upload = TransferManagerBuilder.standard()
                .withS3Client(amazonS3)
                .withMultipartUploadThreshold(100L * Constants.MB)
                .build()
                .upload(s3Bucket,
                        key == null ? getKey(userFilePath) : key,
                        path.toFile());
        if (sync) {
            log.info("Waiting for upload to finish: {}", upload.getDescription());
            upload.waitForUploadResult();
        }
        log.info(upload.getDescription());
    }

    /**
     * Moves a file from one location to another.
     *
     * @param userPath Username as part of s3 object key (thus constructing the path).
     * @param srcPath  Source location.
     * @param dstPath  Destination location.
     */
    public void move(String userPath, Path srcPath, Path dstPath) {
        log.info("Moving {} to {}", getKey(srcPath), getKey(dstPath));
        if (dstPath.toFile().isDirectory()) {
            moveFolder(userPath, srcPath, dstPath);
        } else {
            moveFile(userPath, srcPath, dstPath);
        }
    }

    private void moveFolder(String userPath, Path srcPath, Path dstPath) {
        final var srcFilePath = Path.of(userPath + "/" + srcPath);
        final var destFilePath = Path.of(userPath + "/" + dstPath);
        ObjectListing objectListing = amazonS3.listObjects(s3Bucket, getKey(srcFilePath));
        for (S3ObjectSummary s3ObjectSummary : objectListing.getObjectSummaries()) {
            String srcKey = s3ObjectSummary.getKey();
            String dstKey = getKey(destFilePath) + srcKey.replace(getKey(srcFilePath) + "/",
                    "/");
            amazonS3.copyObject(s3Bucket, srcKey, s3Bucket, dstKey);
            amazonS3.deleteObject(s3Bucket, srcKey);
        }
    }

    private void moveFile(String userPath, Path srcPath, Path dstPath) {
        final var srcFilePath = Path.of(userPath + "/" + srcPath);
        final var destFilePath = Path.of(userPath + "/" + dstPath);
        amazonS3.copyObject(s3Bucket, getKey(srcFilePath), s3Bucket, getKey(destFilePath));
        amazonS3.deleteObject(s3Bucket, getKey(srcFilePath));
    }

    /**
     * Removes file by path.
     *
     * @param userPath Username as part of s3 object key (thus constructing the path).
     * @param path     Path to file to remove.
     */
    public void remove(String userPath, Path path) {
        final var srcFilePath = Path.of(userPath + "/" + path);
        log.info("Removing object {}", getKey(srcFilePath));
        amazonS3.deleteObject(s3Bucket, getKey(srcFilePath));
    }

    /**
     * Converts filesystem <code>path</code> to S3 key.
     *
     * @param path Filesystem <code>path</code>.
     * @return S3 key.
     */
    public String getKey(Path path) {
        String key = path.toString();
        if (key.startsWith("/")) {
            key = key.substring(1);
        }
        return key;
    }

    @Autowired
    public void setAmazonS3(AmazonS3 amazonS3) {
        this.amazonS3 = amazonS3;
    }

    @Value("${inbox.s3.bucket}")
    public void setS3Bucket(String s3Bucket) {
        this.s3Bucket = s3Bucket;
    }
}
