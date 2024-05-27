package no.uio.ifi.localega.doa.repositories;

import no.uio.ifi.localega.doa.model.DatasetEventLog;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.Optional;

@Repository
public interface DatasetEventLogRepository extends JpaRepository<DatasetEventLog, Integer> {

    Optional<DatasetEventLog> findFirstByDatasetIdOrderByEventDateDesc(String datasetId);
}
