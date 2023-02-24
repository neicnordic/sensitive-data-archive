package se.nbis.lega.inbox.s3;

import jakarta.annotation.PostConstruct;
import lombok.extern.slf4j.Slf4j;
import org.apache.sshd.server.session.ServerSession;
import org.apache.sshd.sftp.server.Handle;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.autoconfigure.condition.ConditionalOnExpression;
import org.springframework.stereotype.Component;
import se.nbis.lega.inbox.sftp.InboxSftpEventListener;

import java.io.IOException;
import java.nio.file.CopyOption;
import java.nio.file.Path;
import java.util.Collection;

/**
 * <code>SftpEventListener</code> implementation with support for S3 operations.
 * Optional bean: initialized only if S3 keys are present in the context.
 */
@Slf4j
@ConditionalOnExpression("!'${inbox.s3.access-key}'.isEmpty() && !'${inbox.s3.secret-key}'.isEmpty() && !'${inbox.s3.bucket}'.isEmpty()")
@Component
public class S3SftpEventListener extends InboxSftpEventListener {

    private S3Service s3Service;

    @PostConstruct
    @Override
    public void init() {
        log.info("Initializing {}", this.getClass());
    }

    /**
     * {@inheritDoc}
     */
    @Override
    public void initialized(ServerSession session, int version) {
        s3Service.prepareBucket();
        super.initialized(session, version);
    }

    /**
     * {@inheritDoc}
     */
    @Override
    public void removed(ServerSession session, Path path, boolean isDirectory, Throwable thrown) {
        s3Service.remove(session.getUsername(), path);
        super.removed(session, path, isDirectory, thrown);
    }

    /**
     * {@inheritDoc}
     */
    @Override
    public void moved(ServerSession session, Path srcPath, Path dstPath, Collection<CopyOption> opts, Throwable thrown) {
        s3Service.move(session.getUsername(), srcPath, dstPath);
        super.moved(session, srcPath, dstPath, opts, thrown);
    }

    /**
     * {@inheritDoc}
     */
    @Override
    protected void closed(ServerSession session, String remoteHandle, Handle localHandle) throws IOException, InterruptedException {
        s3Service.upload(session.getUsername(), null, localHandle.getFile(), true);
        super.closed(session, remoteHandle, localHandle);
    }

    @Override
    protected String getFilePath(Path path, String username) {
        final var objectPath = Path.of(username + "/" + path);
        log.debug("S3 object path is {} for user {}", objectPath, username);
        return s3Service.getKey(objectPath);
    }

    @Autowired
    public void setS3Service(S3Service s3Service) {
        this.s3Service = s3Service;
    }

}
