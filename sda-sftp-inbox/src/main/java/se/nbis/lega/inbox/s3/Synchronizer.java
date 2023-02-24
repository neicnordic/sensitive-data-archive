package se.nbis.lega.inbox.s3;

import com.amazonaws.services.s3.AmazonS3;
import com.google.common.collect.HashMultimap;
import com.google.common.collect.Multimap;
import lombok.extern.slf4j.Slf4j;
import org.apache.commons.io.FileUtils;
import org.apache.commons.io.filefilter.TrueFileFilter;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnBean;
import org.springframework.boot.context.event.ApplicationReadyEvent;
import org.springframework.context.ApplicationListener;
import org.springframework.stereotype.Component;

import java.io.File;
import java.util.Collection;

/**
 * Syncronizes local storage with remote storage (S3).
 */
@Slf4j
@ConditionalOnBean(AmazonS3.class)
@Component
public class Synchronizer implements ApplicationListener<ApplicationReadyEvent> {

    private String inboxFolder;

    private S3Service s3Service;

    private String s3Bucket;

    /**
     * {@inheritDoc}
     */
    @Override
    public void onApplicationEvent(ApplicationReadyEvent event) {
        String root;
        if (inboxFolder.endsWith(File.separator)) {
            root = inboxFolder;
        } else {
            root = inboxFolder + File.separator;
        }
        Multimap<String, File> filesPerBuckets = getFilesPerBuckets(root);
        for (String bucket : filesPerBuckets.keySet()) {
            synchronizeBucket(root, filesPerBuckets.get(bucket), bucket);
        }
    }

    private void synchronizeBucket(String root, Collection<File> localFiles, String userPath) {
        log.info("Synchronizing bucket {}", s3Bucket + "/" + userPath);
        s3Service.prepareBucket();
        Collection<String> remoteKeys = s3Service.listKeys(userPath);
        for (File localFile : localFiles) {
            String localKey = getKey(userPath, root, localFile);
            if (!remoteKeys.contains(localKey)) {
                try {
                    s3Service.upload(userPath,
                            getKey(userPath, root, localFile),
                            localFile.toPath(), false);
                } catch (InterruptedException e) {
                    log.error(e.getMessage(), e);
                }
            }
        }
    }

    private Multimap<String, File> getFilesPerBuckets(String root) {
        Collection<File> files = FileUtils.listFiles(new File(root), TrueFileFilter.INSTANCE, TrueFileFilter.INSTANCE);
        Multimap<String, File> filesPerBuckets = HashMultimap.create();
        for (File file : files) {
            String path = file.toString().substring(root.length());
            String bucket = path.split(File.separator)[0];
            filesPerBuckets.put(bucket, file);
        }
        return filesPerBuckets;
    }

    private String getKey(String bucket, String root, File file) {
        return file.toString().substring(root.length() + bucket.length() + 1);
    }

    @Value("${inbox.local.directory}")
    public void setInboxFolder(String inboxFolder) {
        this.inboxFolder = inboxFolder;
    }

    @Autowired
    public void setS3Service(S3Service s3Service) {
        this.s3Service = s3Service;
    }

    @Value("${inbox.s3.bucket}")
    public void setS3Bucket(String s3Bucket) {
        this.s3Bucket = s3Bucket;
    }

}
